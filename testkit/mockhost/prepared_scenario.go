// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/testkit/fakevendor"
	"github.com/arvel-dev/arvel-sdk/testkit/scenario"
)

// PrepareOptions selects borrowed paths and an optional borrowed environment.
type PrepareOptions struct {
	Environment   *Environment
	Tmpdir        string
	FakevendorDir string
	Sandbox       *protocol.SandboxInfo
}

// PreparedScenario resolves static inputs and builds, but never runs, fake CLIs.
// Close it after closing every driver using its Environment.
type PreparedScenario struct {
	environment *Environment
	own         bool
	meta        protocol.InitMeta
	env         map[string]string
	sc          *scenario.Scenario
	resolver    scenario.Resolver
	mu          sync.Mutex
	cleanups    []func() error
	closed      bool
}

// PrepareScenario prepares static initialization and startup environment.
func PrepareScenario(ctx context.Context, sc *scenario.Scenario, opts PrepareOptions) (p *PreparedScenario, err error) {
	if sc == nil {
		return nil, errors.New("mockhost: scenario is nil")
	}
	if _, err := json.Marshal(sc.Initialize.Connection); err != nil {
		return nil, errors.New("mockhost: invalid initialize.connection values")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e := opts.Environment
	own := e == nil
	if own {
		e, err = NewEnvironment(EnvironmentOptions{})
		if err != nil {
			return nil, err
		}
	}
	p = &PreparedScenario{environment: e, own: own, sc: sc}
	prepared := p
	defer func() {
		if err != nil {
			_ = prepared.close(false)
		}
	}()
	tmp := opts.Tmpdir
	if tmp == "" {
		tmp = e.Workspace()
	}
	cache := map[string]string{}
	p.resolver = scenario.Resolver{Vars: map[string]string{}, Tmpdir: tmp, DataDir: e.Initialize().DataDir, Workspace: e.Workspace(), Fakevendor: func(name string) (string, error) {
		if bin, ok := cache[name]; ok {
			return bin, nil
		}
		s, err := fakevendor.LoadFile(filepath.Join(opts.FakevendorDir, name+".yaml"))
		if err != nil {
			return "", errors.New("mockhost: fake executable script is invalid or missing")
		}
		bin, cleanup, err := fakevendor.Build(ctx, s, "fake-"+name)
		if err != nil {
			return "", errors.New("mockhost: fake executable build failed")
		}
		p.cleanups = append(p.cleanups, func() error {
			cleanup()
			if _, err := os.Stat(filepath.Dir(bin)); !os.IsNotExist(err) {
				return errors.New("mockhost: remove fake executable directory")
			}
			return nil
		})
		cache[name] = bin
		return bin, nil
	}}
	m := e.Initialize()
	in := sc.Initialize
	if in.KindSpecified() {
		if in.Connection.Kind == "" {
			return nil, errors.New("mockhost: initialize.connection.kind is empty")
		}
		if in.Connection.Kind != m.Connection.Kind {
			m.Connection = protocol.ConnectionInfo{}
		}
		m.Connection.Kind = in.Connection.Kind
	}
	if m.Connection.ID == "" {
		m.Connection.ID = e.Initialize().Connection.ID
	}
	if in.Connection.ID != "" {
		m.Connection.ID = in.Connection.ID
	}
	if in.Connection.Config != nil {
		cfg := cloneMap(m.Connection.Config)
		if len(in.Connection.Config) == 0 || cfg == nil {
			cfg = map[string]any{}
		}
		for k, v := range in.Connection.Config {
			cfg[k] = cloneValue(v)
		}
		m.Connection.Config = cfg
	}
	if in.Connection.Secrets != nil {
		sec := map[string]string{}
		if len(in.Connection.Secrets) > 0 {
			for k, v := range m.Connection.Secrets {
				sec[k] = v
			}
		}
		for k, v := range in.Connection.Secrets {
			sec[k] = v
		}
		m.Connection.Secrets = sec
	}
	if in.DataDir != "" {
		m.DataDir = in.DataDir
	}
	if opts.Sandbox != nil {
		cp := *opts.Sandbox
		m.Sandbox = &cp
	}
	resolve := func(s string) (string, error) { return p.resolver.ResolveString(s) }
	if m.DataDir, err = resolve(m.DataDir); err != nil {
		return nil, errors.New("mockhost: initialize.dataDir placeholder")
	}
	workspace := e.Workspace()
	if sc.Workspace != "" {
		workspace, err = resolve(sc.Workspace)
		if err != nil {
			return nil, errors.New("mockhost: workspace placeholder")
		}
	}
	if !filepath.IsAbs(m.DataDir) || !filepath.IsAbs(workspace) {
		return nil, errors.New("mockhost: dataDir and workspace must be absolute")
	}
	for _, path := range []string{m.DataDir, workspace} {
		if st, err := os.Stat(path); err != nil || !st.IsDir() {
			return nil, errors.New("mockhost: dataDir and workspace must name existing directories")
		}
	}
	if !own && (!sameDirectory(m.DataDir, e.Initialize().DataDir) || !sameDirectory(workspace, e.Workspace())) {
		return nil, errors.New("mockhost: scenario paths conflict with borrowed environment")
	}
	if own && (m.DataDir != e.Initialize().DataDir || workspace != e.Workspace()) {
		replacement, err := NewEnvironment(EnvironmentOptions{Initialize: m, Workspace: workspace})
		if err != nil {
			return nil, err
		}
		// Keep the initial owned root for any placeholders that reference it.
		old := e
		p.cleanups = append(p.cleanups, old.Close)
		p.environment = replacement
	}
	p.resolver.DataDir = m.DataDir
	p.resolver.Workspace = workspace
	if opts.Tmpdir == "" {
		p.resolver.Tmpdir = workspace
	}
	if m.Connection.Kind, err = resolve(m.Connection.Kind); err != nil {
		return nil, errors.New("mockhost: initialize.connection.kind placeholder")
	}
	if m.Connection.ID, err = resolve(m.Connection.ID); err != nil {
		return nil, errors.New("mockhost: initialize.connection.id placeholder")
	}
	cfg, err := p.resolver.Resolve(m.Connection.Config)
	if err != nil {
		return nil, errors.New("mockhost: initialize.connection.config placeholder")
	}
	m.Connection.Config = cfg.(map[string]any)
	sec, err := p.resolver.Resolve(m.Connection.Secrets)
	if err != nil {
		return nil, errors.New("mockhost: initialize.connection.secrets placeholder")
	}
	m.Connection.Secrets = sec.(map[string]string)
	p.meta = cloneMeta(m)
	env, err := p.resolver.Resolve(sc.Plugin.Env)
	if err != nil {
		return nil, errors.New("mockhost: plugin.env placeholder")
	}
	p.env = env.(map[string]string)
	// Fake names are literal file stems, so compile step dependencies now too.
	// Saved variables remain unresolved until execution; no build outlives the
	// preparation context or runs under a cancelled scenario context later.
	for _, step := range sc.Steps {
		if err := prepareStepFakes(step.Params, p.resolver); err != nil {
			return nil, errors.New("mockhost: step params contain invalid fake placeholder")
		}
	}
	return p, nil
}

func prepareStepFakes(v any, r scenario.Resolver) error {
	switch x := v.(type) {
	case string:
		for {
			start := strings.Index(x, "{{")
			if start < 0 {
				return nil
			}
			end := strings.Index(x[start:], "}}")
			if end < 0 {
				return errors.New("unterminated placeholder")
			}
			token := x[start : start+end+2]
			name := strings.TrimSpace(x[start+2 : start+end])
			if strings.HasPrefix(name, "fakevendor:") {
				if _, err := r.ResolveString(token); err != nil {
					return err
				}
			}
			x = x[start+end+2:]
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			if err := prepareStepFakes(x[key], r); err != nil {
				return err
			}
		}
	case []any:
		for _, value := range x {
			if err := prepareStepFakes(value, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func sameDirectory(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	x, e1 := os.Stat(a)
	y, e2 := os.Stat(b)
	return e1 == nil && e2 == nil && os.SameFile(x, y)
}

// Environment returns the context to borrow when starting the plugin.
func (p *PreparedScenario) Environment() *Environment { return p.environment }

// Initialize returns a cloned resolved initialization snapshot.
func (p *PreparedScenario) Initialize() protocol.InitMeta { return cloneMeta(p.meta) }

// Env returns a copy of startup overrides, without implicit parent variables.
func (p *PreparedScenario) Env() map[string]string {
	o := make(map[string]string, len(p.env))
	for k, v := range p.env {
		o[k] = v
	}
	return o
}

// Close releases prepared resources only after driver termination is confirmed.
func (p *PreparedScenario) Close() error { return p.close(true) }

func (p *PreparedScenario) close(published bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if published && p.environment.inUse() {
		return errors.New("mockhost: prepared environment is still in use")
	}
	if p.closed {
		return nil
	}
	for i := len(p.cleanups) - 1; i >= 0; i-- {
		if err := p.cleanups[i](); err != nil {
			return err
		}
	}
	if p.own {
		if err := p.environment.Close(); err != nil {
			return err
		}
	}
	p.closed = true
	return nil
}

// Run initializes the already-started driver and executes dynamic steps.
func (p *PreparedScenario) Run(ctx context.Context, d *Driver) (ScenarioResult, error) {
	if d.environment != p.environment {
		return ScenarioResult{}, errors.New("mockhost: driver environment does not match preparation")
	}
	if _, err := d.InitializeCtx(ctx, p.Initialize()); err != nil {
		return ScenarioResult{}, safeCallError("initialize", err)
	}
	return p.RunInitialized(ctx, d)
}

// RunInitialized executes steps after the caller has initialized the driver.
// This allows a checker to add controlled canary metadata before initialization.
func (p *PreparedScenario) RunInitialized(ctx context.Context, d *Driver) (ScenarioResult, error) {
	if d.environment != p.environment {
		return ScenarioResult{}, errors.New("mockhost: driver environment does not match preparation")
	}
	return d.runSteps(ctx, p.sc, p.resolver)
}
