// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/testkit/scenario"
)

type environmentProvider struct {
	*stubProvider
	meta protocol.InitMeta
	cwd  []string
}

func (p *environmentProvider) Initialize(_ context.Context, m protocol.InitMeta) (plugin.Features, error) {
	p.meta = m
	return plugin.Features{LoadSession: true}, nil
}
func (p *environmentProvider) NewSession(_ context.Context, in plugin.SessionParams) (plugin.Session, error) {
	p.cwd = append(p.cwd, in.CWD)
	return &stubSession{id: "s1"}, nil
}
func (p *environmentProvider) LoadSession(_ context.Context, in plugin.LoadParams) (plugin.Session, error) {
	p.cwd = append(p.cwd, in.CWD)
	return &stubSession{id: in.SessionID}, nil
}

func TestEnvironment_OwnershipAndStablePaths(t *testing.T) {
	borrowed := t.TempDir()
	sentinel := filepath.Join(borrowed, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	e, err := NewEnvironment(EnvironmentOptions{Initialize: protocol.InitMeta{DataDir: borrowed}})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	p := &environmentProvider{stubProvider: streamingProvider()}
	h := InProcessWithEnvironment(t, p, e)
	if e.Close() == nil {
		t.Fatal("closed an environment still in use")
	}
	if _, err = h.Initialize(protocol.InitMeta{Connection: protocol.ConnectionInfo{Kind: "explicit"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.NewSession(protocol.SessionMeta{}); err != nil {
		t.Fatal(err)
	}
	if err = h.LoadSession("restored", protocol.SessionMeta{}); err != nil {
		t.Fatal(err)
	}
	if err = h.CloseWithError(); err != nil {
		t.Fatal(err)
	}
	if p.meta.DataDir != borrowed || p.meta.Connection.Kind != "explicit" {
		t.Fatal("explicit metadata changed")
	}
	if len(p.cwd) != 2 || p.cwd[0] != p.cwd[1] || p.cwd[0] != e.Workspace() || !filepath.IsAbs(p.cwd[0]) {
		t.Fatal("workspace was not stable and absolute")
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(e.Root()); !os.IsNotExist(err) {
		t.Fatal("owned root retained")
	}
	if _, err = os.Stat(sentinel); err != nil {
		t.Fatal("borrowed data removed")
	}
}

func TestEnvironment_ConcurrentIsolation(t *testing.T) {
	var wg sync.WaitGroup
	paths := make(chan string, 16)
	for range 8 {
		wg.Go(func() {
			e, err := NewEnvironment(EnvironmentOptions{})
			if err != nil {
				t.Error(err)
				return
			}
			paths <- e.Initialize().DataDir
			paths <- e.Workspace()
			if err := e.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	close(paths)
	seen := map[string]bool{}
	for path := range paths {
		if seen[path] || !filepath.IsAbs(path) {
			t.Fatal("paths not isolated")
		}
		seen[path] = true
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("owned path retained")
		}
	}
}

func TestPrepareScenario_TypesAndOwnership(t *testing.T) {
	sc, err := scenario.Load(strings.NewReader(`name: typed
initialize:
  connection:
    kind: synthetic
    config: { enabled: false, count: 2, nested: [true, 3, "{{workspace}}"] }
    secrets: { token: "{{dataDir}}" }
plugin:
  env: { FIXTURE: "{{workspace}}" }
steps: [{call: _arvel/status}]
`))
	if err != nil {
		t.Fatal(err)
	}
	before := sc.Initialize.Connection.Config["nested"].([]any)[2]
	p, err := PrepareScenario(context.Background(), sc, PrepareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	m := p.Initialize()
	if m.Connection.Config["enabled"] != false || m.Connection.Config["count"] != 2 {
		t.Fatal("scalar types changed")
	}
	if m.Connection.Secrets["token"] != m.DataDir || p.Env()["FIXTURE"] != p.Environment().Workspace() {
		t.Fatal("static placeholders unresolved")
	}
	if sc.Initialize.Connection.Config["nested"].([]any)[2] != before {
		t.Fatal("mutated input")
	}
	cp := p.Initialize()
	cp.Connection.Config["count"] = 99
	if reflect.DeepEqual(cp, p.Initialize()) {
		t.Fatal("snapshot aliases original")
	}
	if err = p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareScenario_RejectsUnsafeStaticInputs(t *testing.T) {
	for _, value := range []string{"{{session}}", "secret{{unfinished", "{{fakevendor:../escape}}", "{{fakevendor:C:\\escape}}"} {
		t.Run("invalid", func(t *testing.T) {
			sc := &scenario.Scenario{Initialize: scenario.Initialize{Connection: protocol.ConnectionInfo{Secrets: map[string]string{"token": value}}}}
			_, err := PrepareScenario(context.Background(), sc, PrepareOptions{})
			if err == nil {
				t.Fatal("unsafe static input accepted")
			}
			if strings.Contains(err.Error(), value) {
				t.Fatal("diagnostic exposed input")
			}
		})
	}
	e, err := NewEnvironment(EnvironmentOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	_, err = PrepareScenario(context.Background(), &scenario.Scenario{Workspace: t.TempDir()}, PrepareOptions{Environment: e})
	if err == nil {
		t.Fatal("borrowed path conflict accepted")
	}
}

func TestEnvironment_SnapshotsCloneTypedContainers(t *testing.T) {
	config := map[string]any{"map": map[string]string{"value": "original"}, "slice": []string{"original"}}
	e, err := NewEnvironment(EnvironmentOptions{Initialize: protocol.InitMeta{Connection: protocol.ConnectionInfo{Config: config}}})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	config["map"].(map[string]string)["value"] = "mutated"
	snap := e.Initialize()
	snap.Connection.Config["map"].(map[string]string)["value"] = "snapshot mutation"
	snap.Connection.Config["slice"].([]string)[0] = "snapshot mutation"
	current := e.Initialize().Connection.Config
	if current["map"].(map[string]string)["value"] != "original" || current["slice"].([]string)[0] != "original" {
		t.Fatal("snapshot shared caller memory")
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	if _, err := NewEnvironment(EnvironmentOptions{Initialize: protocol.InitMeta{Connection: protocol.ConnectionInfo{Config: cycle}}}); err == nil {
		t.Fatal("cyclic non-JSON input accepted")
	}
}

func TestPrepareScenario_BorrowedConnectionOverlay(t *testing.T) {
	e, err := NewEnvironment(EnvironmentOptions{Initialize: protocol.InitMeta{Connection: protocol.ConnectionInfo{Kind: "synthetic", Config: map[string]any{"base": true}, Secrets: map[string]string{"base": "synthetic"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	sc := &scenario.Scenario{Initialize: scenario.Initialize{Connection: protocol.ConnectionInfo{Kind: "synthetic", Config: map[string]any{"count": 2}, Secrets: map[string]string{"other": "fixture"}}}}
	p, err := PrepareScenario(context.Background(), sc, PrepareOptions{Environment: e})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	got := p.Initialize().Connection
	if got.Config["base"] != true || got.Config["count"] != 2 || len(got.Secrets) != 2 {
		t.Fatal("same-kind defaults lost")
	}
}

func TestPrepareScenario_SelectedDirectoryPlaceholders(t *testing.T) {
	data, workspace := t.TempDir(), t.TempDir()
	sc := &scenario.Scenario{Workspace: workspace, Initialize: scenario.Initialize{DataDir: data, Connection: protocol.ConnectionInfo{Config: map[string]any{"cwd": "{{workspace}}"}, Secrets: map[string]string{"token": "{{dataDir}}"}}}, Plugin: scenario.Plugin{Env: map[string]string{"CWD": "{{workspace}}", "TMP": "{{tmpdir}}"}}}
	p, err := PrepareScenario(context.Background(), sc, PrepareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Initialize().Connection.Config["cwd"] != workspace || p.Initialize().Connection.Secrets["token"] != data || p.Env()["CWD"] != workspace || p.Env()["TMP"] != workspace {
		t.Fatal("placeholders used discarded default directories")
	}
}

func TestPrepareScenario_FakeBuildAndPartialCleanup(t *testing.T) {
	scratch := filepath.Join(t.TempDir(), "temporary files")
	if err := os.Mkdir(scratch, 0700); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, scratch)
	}
	scripts := t.TempDir()
	if os.WriteFile(filepath.Join(scripts, "hold.yaml"), []byte("name: hold\nsteps: [{sleepMillis: 120000}]\n"), 0600) != nil {
		t.Fatal("write script")
	}
	t.Run("build does not execute and stays until close", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		sc := &scenario.Scenario{Initialize: scenario.Initialize{Connection: protocol.ConnectionInfo{Config: map[string]any{"binary": "{{fakevendor:hold}}"}}}}
		p, err := PrepareScenario(ctx, sc, PrepareOptions{FakevendorDir: scripts})
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		bin := p.Initialize().Connection.Config["binary"].(string)
		if _, err = os.Stat(bin); err != nil {
			t.Fatal("fake was not compiled")
		}
		h := InProcessWithEnvironment(t, streamingProvider(), p.Environment())
		if p.Close() == nil {
			t.Fatal("live preparation closed")
		}
		if err = h.CloseWithError(); err != nil {
			t.Fatal(err)
		}
		if err = p.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err = os.Stat(bin); !os.IsNotExist(err) {
			t.Fatal("fake survived owner close")
		}
	})
	t.Run("error after build with active borrowed context", func(t *testing.T) {
		e, err := NewEnvironment(EnvironmentOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer e.Close()
		h := InProcessWithEnvironment(t, streamingProvider(), e)
		sc := &scenario.Scenario{Initialize: scenario.Initialize{Connection: protocol.ConnectionInfo{Config: map[string]any{"a": "{{fakevendor:hold}}", "z": "{{missing}}"}}}}
		if _, err = PrepareScenario(context.Background(), sc, PrepareOptions{Environment: e, FakevendorDir: scripts}); err == nil {
			t.Fatal("bad placeholder accepted")
		}
		fakes, err := filepath.Glob(filepath.Join(scratch, "fakevendor-*"))
		if err != nil || len(fakes) != 0 {
			t.Fatal("unpublished fake leaked after preparation failure")
		}
		if err = h.CloseWithError(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestScenario_DiagnosticsOmitPayload(t *testing.T) {
	h := InProcess(t, streamingProvider())
	sc := &scenario.Scenario{Name: "mismatch", Steps: []scenario.Step{{Call: protocol.MethodStatus, Expect: map[string]any{"message": "sensitive-input"}}}}
	res, err := h.core.runScenario(sc, ScenarioOptions{Tmpdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pass || strings.Contains(res.Steps[0].Err, "sensitive-input") {
		t.Fatal("mismatch diagnostic unsafe")
	}
}
