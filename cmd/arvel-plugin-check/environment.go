// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/arvel-dev/arvel-sdk/manifest"
	"github.com/arvel-dev/arvel-sdk/procx"
	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/testkit/mockhost"
	"github.com/arvel-dev/arvel-sdk/testkit/scenario"
	acp "github.com/coder/acp-go-sdk"
	"gopkg.in/yaml.v3"
)

type testSettings struct {
	Initialize scenario.Initialize `yaml:"initialize"`
	Workspace  string              `yaml:"workspace"`
	Plugin     scenario.Plugin     `yaml:"plugin"`
	Prompt     []map[string]any    `yaml:"prompt"`
}
type testConfiguration struct {
	Initialize scenario.Initialize     `yaml:"initialize"`
	Workspace  string                  `yaml:"workspace"`
	Plugin     scenario.Plugin         `yaml:"plugin"`
	Checks     map[string]testSettings `yaml:"checks"`
}

func loadTestConfiguration(file string) (*testConfiguration, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, errors.New("test environment: cannot open file")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return nil, errors.New("test environment: file exceeds limit or cannot be read")
	}
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	var cfg testConfiguration
	if dec.Decode(&cfg) != nil {
		return nil, errors.New("test environment: invalid YAML, unknown field or field type")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return nil, errors.New("test environment: expected one YAML document")
	}
	for name, settings := range cfg.Checks {
		switch name {
		case "basic", "concurrency", "cancel", "busy", "route-validity", "proxy":
		default:
			return nil, errors.New("test environment: unknown checks field")
		}
		if settings.Prompt != nil && name != "concurrency" && name != "cancel" && name != "busy" {
			return nil, errors.New("test environment: prompt is only valid for session probes")
		}
	}
	return &cfg, nil
}

func mergeSettings(base, over testSettings) testSettings {
	a, b := base.Initialize.Connection, over.Initialize.Connection
	if over.Initialize.KindSpecified() && b.Kind != a.Kind {
		a = protocol.ConnectionInfo{}
	}
	if over.Initialize.KindSpecified() {
		a.Kind = b.Kind
	}
	if b.ID != "" {
		a.ID = b.ID
	}
	if b.Config != nil {
		a.Config = mergeConfig(a.Config, b.Config)
	} else {
		a.Config = mergeConfig(nil, a.Config)
	}
	if b.Secrets != nil {
		out := map[string]string{}
		if len(b.Secrets) > 0 {
			for k, v := range a.Secrets {
				out[k] = v
			}
		}
		for k, v := range b.Secrets {
			out[k] = v
		}
		a.Secrets = out
	} else if a.Secrets != nil {
		out := map[string]string{}
		for k, v := range a.Secrets {
			out[k] = v
		}
		a.Secrets = out
	}
	base.Initialize.Connection = a
	if over.Initialize.DataDir != "" {
		base.Initialize.DataDir = over.Initialize.DataDir
	}
	if over.Workspace != "" {
		base.Workspace = over.Workspace
	}
	if over.Plugin.Env != nil {
		out := map[string]string{}
		if len(over.Plugin.Env) > 0 {
			for k, v := range base.Plugin.Env {
				out[environmentKey(k)] = v
			}
		}
		for k, v := range over.Plugin.Env {
			out[environmentKey(k)] = v
		}
		base.Plugin.Env = out
	}

	if over.Prompt != nil {
		base.Prompt = over.Prompt
	}
	return base
}
func environmentKey(k string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(k)
	}
	return k
}
func validateEnvironmentKeys(env map[string]string) error {
	seen := map[string]bool{}
	for k := range env {
		n := environmentKey(k)
		if seen[n] {
			return errors.New("plugin.env: duplicate native variable name")
		}
		seen[n] = true
	}
	return nil
}

func mergeConfig(a, b map[string]any) map[string]any {
	if b == nil && a == nil {
		return nil
	}
	out := map[string]any{}
	if b == nil || len(b) > 0 {
		for k, v := range a {
			out[k] = v
		}
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func connectionDefaults(m *manifest.Manifest, meta *protocol.InitMeta, explicit bool) error {
	if m == nil {
		return nil
	} // Direct check unit tests may omit the manifest.
	if !explicit {
		if len(m.Connections) != 1 {
			return errors.New("initialize.connection.kind: explicit test selection required")
		}
		c := m.Connections[0]
		for _, s := range c.Secrets {
			if s.Required {
				return errors.New("initialize.connection.kind: explicit test selection and secrets required")
			}
		}
		for _, p := range c.Config.Properties {
			if p.Default == nil {
				return errors.New("initialize.connection.kind: explicit test configuration required")
			}
		}
		meta.Connection.Kind = c.Kind
	}
	var selected *manifest.Connection
	for i := range m.Connections {
		if m.Connections[i].Kind == meta.Connection.Kind {
			selected = &m.Connections[i]
			break
		}
	}
	if selected == nil {
		return errors.New("initialize.connection.kind: undeclared kind")
	}
	if meta.Connection.Config == nil {
		meta.Connection.Config = map[string]any{}
	}
	for name, p := range selected.Config.Properties {
		v, ok := meta.Connection.Config[name]
		if !ok {
			if p.Default == nil {
				continue
			}
			v = p.Default
			meta.Connection.Config[name] = v
		}
		valid := true
		switch p.Type {
		case "boolean":
			_, valid = v.(bool)
		case "string":
			s, ok := v.(string)
			valid = ok
			if valid && len(p.Enum) > 0 {
				valid = false
				for _, e := range p.Enum {
					valid = valid || s == e
				}
			}
			if valid && p.Pattern != "" {
				valid, _ = regexp.MatchString(p.Pattern, s)
			}
		case "integer":
			b, _ := json.Marshal(v)
			var n int64
			valid = json.Unmarshal(b, &n) == nil
			if valid && p.Minimum != nil {
				valid = n >= *p.Minimum
			}
			if valid && p.Maximum != nil {
				valid = n <= *p.Maximum
			}
		}
		if !valid {
			return fmt.Errorf("initialize.connection.config.%s: invalid declared field", safeField(name))
		}
	}
	for _, s := range selected.Secrets {
		value := meta.Connection.Secrets[s.ID]
		if s.Required && value == "" {
			return fmt.Errorf("initialize.connection.secrets.%s: missing required field", safeField(s.ID))
		}
		if value != "" && s.Pattern != "" {
			if ok, _ := regexp.MatchString(s.Pattern, value); !ok {
				return fmt.Errorf("initialize.connection.secrets.%s: invalid declared field", safeField(s.ID))
			}
		}
	}
	return nil
}

type checkRun struct {
	d           *mockhost.Driver
	prepared    *mockhost.PreparedScenario
	meta        protocol.InitMeta
	canary      string
	prompt      []acp.ContentBlock
	strict      bool
	ctx         context.Context
	stop        func() bool
	contextDone chan struct{}
}

func prepareCheck(ctx context.Context, f *flags, name string, sc *scenario.Scenario, fakeDir string, proxy string) (r *checkRun, err error) {
	cfg := f.config
	if cfg == nil && f.testEnv != "" {
		cfg, err = loadTestConfiguration(f.testEnv)
		if err != nil {
			return nil, err
		}
	}
	var settings testSettings
	validateInit := func(in scenario.Initialize) error {
		if in.KindSpecified() && in.Connection.Kind == "" {
			return errors.New("initialize.connection.kind: empty kind")
		}
		return nil
	}
	if cfg != nil {
		if err := validateInit(cfg.Initialize); err != nil {
			return nil, err
		}
		if err := validateInit(cfg.Checks[name].Initialize); err != nil {
			return nil, err
		}
	}
	if sc != nil {
		if err := validateInit(sc.Initialize); err != nil {
			return nil, err
		}
	}
	if cfg != nil {
		if err := validateEnvironmentKeys(cfg.Plugin.Env); err != nil {
			return nil, err
		}
		if err := validateEnvironmentKeys(cfg.Checks[name].Plugin.Env); err != nil {
			return nil, err
		}
		settings = testSettings{Initialize: cfg.Initialize, Workspace: cfg.Workspace, Plugin: cfg.Plugin}
		settings = mergeSettings(settings, cfg.Checks[name])
	}
	if sc != nil {
		if err := validateEnvironmentKeys(sc.Plugin.Env); err != nil {
			return nil, err
		}
		settings = mergeSettings(settings, testSettings{Initialize: sc.Initialize, Workspace: sc.Workspace, Plugin: sc.Plugin})
	}
	if settings.Initialize.KindSpecified() && settings.Initialize.Connection.Kind == "" {
		return nil, errors.New("initialize.connection.kind: empty kind")
	}
	if _, exists := settings.Initialize.Connection.Secrets["conformance-canary"]; exists {
		return nil, errors.New("initialize.connection.secrets: reserved canary field")
	}
	preparedSC := &scenario.Scenario{Name: name, Initialize: settings.Initialize, Workspace: settings.Workspace, Plugin: settings.Plugin}
	if sc != nil {
		preparedSC.Name = sc.Name
		preparedSC.Steps = sc.Steps
	}
	opts := mockhost.PrepareOptions{FakevendorDir: fakeDir}
	if proxy != "" {
		opts.Sandbox = &protocol.SandboxInfo{Mode: protocol.SandboxModeOS, Proxy: proxy}
	}
	p, err := mockhost.PrepareScenario(ctx, preparedSC, opts)
	if err != nil {
		return nil, err
	}
	r = &checkRun{prepared: p, meta: p.Initialize(), canary: newCanary(), ctx: ctx, strict: cfg != nil || settings.Prompt != nil, prompt: []acp.ContentBlock{acp.TextBlock("hi")}}
	pending := r
	defer func() {
		if err != nil {
			_ = pending.prepared.Close()
		}
	}()
	m := f.loaded
	if m == nil && f.manifest != "" {
		m, err = manifest.LoadFile(f.manifest)
		if err != nil {
			return nil, errors.New("manifest: cannot load test context")
		}
	}
	if err = connectionDefaults(m, &r.meta, settings.Initialize.KindSpecified()); err != nil {
		return nil, err
	}
	if r.meta.Connection.Secrets == nil {
		r.meta.Connection.Secrets = map[string]string{}
	}
	r.meta.Connection.Secrets["conformance-canary"] = r.canary
	if settings.Prompt != nil {
		raw, e := json.Marshal(settings.Prompt)
		if e != nil {
			return nil, errors.New("checks.prompt: invalid content blocks")
		}
		if json.Unmarshal(raw, &r.prompt) != nil || len(r.prompt) == 0 {
			return nil, errors.New("checks.prompt: invalid content blocks")
		}
		for _, b := range r.prompt {
			if e := b.Validate(); e != nil {
				return nil, errors.New("checks.prompt: invalid content block")
			}
		}
	}
	env, err := checkEnvironment(p, cfg != nil, proxy != "")
	if err != nil {
		return nil, err
	}
	path, err := exec.LookPath(f.binary)
	if err != nil {
		return nil, errors.New("plugin binary: cannot resolve executable")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, errors.New("plugin binary: cannot resolve absolute path")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	r.d, err = mockhost.StartDriver(path, mockhost.WithArgs(f.args...), mockhost.WithEnvironment(p.Environment()), mockhost.WithDir(r.meta.DataDir), mockhost.WithEnv(env))
	if err != nil {
		return nil, errors.New("plugin binary: start failed")
	}
	r.contextDone = make(chan struct{})
	r.stop = context.AfterFunc(ctx, func() { defer close(r.contextDone); _ = r.d.CloseWithError() })
	return r, nil
}

func checkEnvironment(p *mockhost.PreparedScenario, isolated, proxy bool) ([]string, error) {
	values := map[string]string{}
	norm := func(k string) string {
		if runtime.GOOS == "windows" {
			return strings.ToUpper(k)
		}
		return k
	}
	base := os.Environ()
	if isolated {
		base = procx.Env().Build()
	}
	controlled := func(k string) bool {
		k = strings.ToUpper(k)
		switch k {
		case "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TMPDIR", "TMP", "TEMP":
			return true
		}
		return strings.HasPrefix(k, "XDG_")
	}
	proxyVar := func(k string) bool {
		switch strings.ToUpper(k) {
		case "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY", "SSH_AUTH_SOCK":
			return true
		}
		return false
	}
	for _, entry := range base {
		k, v, ok := strings.Cut(entry, "=")
		if !ok || k == "" {
			continue
		}
		if isolated && controlled(k) || proxy && proxyVar(k) {
			continue
		}
		values[norm(k)] = v
	}
	for k, v := range p.Env() {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
			return nil, errors.New("plugin.env: invalid variable")
		}
		if isolated && controlled(k) || proxy && proxyVar(k) || strings.EqualFold(k, "ARVEL_PLUGIN_DATA_DIR") {
			return nil, errors.New("plugin.env: controlled variable cannot be overridden")
		}
		for _, forbidden := range procx.NeverPass() {
			if isolated && strings.EqualFold(k, forbidden) {
				return nil, errors.New("plugin.env: forbidden variable")
			}
		}
		values[norm(k)] = v
	}
	if isolated {
		for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP", "TMPDIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "XDG_CONFIG_DIRS", "XDG_DATA_DIRS"} {
			leaf := strings.ToLower(name)
			if name == "HOME" || name == "USERPROFILE" {
				leaf = "home"
			}
			dir := filepath.Join(p.Environment().Root(), leaf)
			if err := os.MkdirAll(dir, 0700); err != nil {
				return nil, errors.New("plugin.env: cannot allocate private directory")
			}
			values[norm(name)] = dir
		}
	}
	values[norm("ARVEL_PLUGIN_DATA_DIR")] = p.Initialize().DataDir
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+values[k])
	}
	return out, nil
}

func (r *checkRun) initialize() (protocol.InitResultMeta, error) {
	ctx, cancel := context.WithTimeout(r.ctx, initTimeout)
	defer cancel()
	return r.d.InitializeCtx(ctx, r.meta)
}
func (r *checkRun) ready() error {
	ctx, cancel := context.WithTimeout(r.ctx, initTimeout)
	defer cancel()
	var st protocol.Status
	if err := r.d.Call(ctx, protocol.MethodStatus, nil, &st); err != nil {
		return errors.New("status: test readiness failed")
	}
	if st.State != protocol.StateReady {
		return errors.New("status: test connection must be ready before sessions")
	}
	return nil
}
func (r *checkRun) call(method string, params, result any) error {
	ctx, cancel := context.WithTimeout(r.ctx, 10*time.Second)
	defer cancel()
	return r.d.Call(ctx, method, params, result)
}
func (r *checkRun) newSession() (string, error) {
	var res struct {
		SessionID string `json:"sessionId"`
	}
	err := r.call(protocol.MethodSessionNew, map[string]any{"cwd": r.prepared.Environment().Workspace(), "mcpServers": []any{}, "_meta": protocol.WithMeta(nil, protocol.SessionMeta{})}, &res)
	return res.SessionID, err
}
func (r *checkRun) promptSession(id string) (mockhost.PromptResult, error) {
	var out struct {
		StopReason acp.StopReason `json:"stopReason"`
	}
	err := r.call(protocol.MethodSessionPrompt, map[string]any{"sessionId": id, "prompt": r.prompt}, &out)
	return mockhost.PromptResult{StopReason: out.StopReason}, err
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	var rpc *protocol.RPCError
	if errors.As(err, &rpc) {
		return fmt.Sprintf("RPC code %d", rpc.Code)
	}
	if errors.Is(err, context.Canceled) {
		return "operation cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline exceeded"
	}
	return err.Error()
}
func (r *checkRun) finish(rows *[]CheckResult) {
	if r.stop != nil && !r.stop() {
		<-r.contextDone
	}
	err := r.d.CloseWithError()
	if err != nil {
		*rows = append(*rows, fail("teardown", safeError(err)))
	}
	if leak := checkCanaryLeak(r.canary, r.d.Stdout()+r.d.Stderr()); leak.Status == StatusFail {
		*rows = append(*rows, leak)
	}
	if err == nil {
		if e := r.prepared.Close(); e != nil {
			*rows = append(*rows, fail("teardown", safeError(e)))
		}
	}
	sensitive := []string{r.canary}
	for _, v := range r.meta.Connection.Secrets {
		sensitive = append(sensitive, v)
	}
	secrets := append([]string(nil), sensitive...)
	collectStrings(r.meta.Connection.Config, &sensitive)
	for _, v := range r.prepared.Env() {
		sensitive = append(sensitive, v)
	}
	for i := range *rows {
		(*rows)[i].Name = scrub((*rows)[i].Name, secrets)
		(*rows)[i].Detail = scrub((*rows)[i].Detail, sensitive)
	}
}
func (r *checkRun) finishOne(result *CheckResult) {
	rows := []CheckResult{*result}
	r.finish(&rows)
	if len(rows) > 1 {
		*result = fail(result.Name, rows[1].Detail)
	} else {
		*result = rows[0]
	}
}
func collectStrings(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case map[string]any:
		for _, v := range x {
			collectStrings(v, out)
		}
	case []any:
		for _, v := range x {
			collectStrings(v, out)
		}
	}
}
func scrub(s string, values []string) string {
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, v := range values {
		if v != "" {
			s = strings.ReplaceAll(s, v, "[redacted]")
			q, _ := json.Marshal(v)
			if len(q) > 2 {
				s = strings.ReplaceAll(s, string(q[1:len(q)-1]), "[redacted]")
			}
		}
	}
	if len(s) > 1024 {
		s = s[:1024] + "..."
	}
	return s
}

func safeField(name string) string {
	if len(name) > 64 {
		return "field"
	}
	for _, c := range name {
		if c != '_' && c != '-' && c != '.' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return "field"
		}
	}
	return name
}
