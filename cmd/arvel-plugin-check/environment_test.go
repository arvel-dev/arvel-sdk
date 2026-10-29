// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/manifest"
	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/testkit/mockhost"
	"github.com/arvel-dev/arvel-sdk/testkit/scenario"
	"gopkg.in/yaml.v3"
)

func TestEnvironment_MergePrecedence(t *testing.T) {
	base := testSettings{Initialize: scenario.Initialize{Connection: protocol.ConnectionInfo{Kind: "one", Config: map[string]any{"a": 1}, Secrets: map[string]string{"token": "old"}}}}
	decode := func(s string) testSettings {
		var out testSettings
		if err := yaml.Unmarshal([]byte(s), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	same := mergeSettings(base, decode("initialize: {connection: {config: {b: false}}}"))
	if same.Initialize.Connection.Config["a"] != 1 || same.Initialize.Connection.Config["b"] != false {
		t.Fatal("same-kind merge failed")
	}
	cleared := mergeSettings(base, decode("initialize: {connection: {config: {}, secrets: {}}}"))
	if len(cleared.Initialize.Connection.Config) != 0 || len(cleared.Initialize.Connection.Secrets) != 0 {
		t.Fatal("explicit empty maps did not clear")
	}
	changed := mergeSettings(base, decode("initialize: {connection: {kind: two}}"))
	if len(changed.Initialize.Connection.Secrets) != 0 || len(changed.Initialize.Connection.Config) != 0 {
		t.Fatal("kind change carried configuration")
	}
	if len(base.Initialize.Connection.Config) != 1 {
		t.Fatal("input mutated")
	}
}

func TestEnvironment_ConnectionSelection(t *testing.T) {
	required := manifest.Connection{Kind: "synthetic", Secrets: []manifest.Secret{{ID: "token", Required: true}}}
	for _, tc := range []struct {
		name            string
		m               manifest.Manifest
		meta            protocol.InitMeta
		explicit, valid bool
	}{
		{"one empty kind", manifest.Manifest{Connections: []manifest.Connection{{Kind: "synthetic"}}}, protocol.InitMeta{}, false, true},
		{"required input", manifest.Manifest{Connections: []manifest.Connection{required}}, protocol.InitMeta{}, false, false},
		{"unknown", manifest.Manifest{Connections: []manifest.Connection{required}}, protocol.InitMeta{Connection: protocol.ConnectionInfo{Kind: "other"}}, true, false},
		{"missing secret", manifest.Manifest{Connections: []manifest.Connection{required}}, protocol.InitMeta{Connection: protocol.ConnectionInfo{Kind: "synthetic"}}, true, false},
		{"configured", manifest.Manifest{Connections: []manifest.Connection{required}}, protocol.InitMeta{Connection: protocol.ConnectionInfo{Kind: "synthetic", Secrets: map[string]string{"token": "synthetic"}}}, true, true},
		{"multiple", manifest.Manifest{Connections: []manifest.Connection{{Kind: "one"}, {Kind: "two"}}}, protocol.InitMeta{}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := connectionDefaults(&tc.m, &tc.meta, tc.explicit)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, want %v", err == nil, tc.valid)
			}
		})
	}
}

func TestEnvironment_IsolatedAliasesAndProxy(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "inherited-sentinel")
	if err := os.Mkdir(sentinel, 0700); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_CONFIG_DIRS", "XDG_DATA_DIRS", "XDG_UNKNOWN", "HTTP_PROXY"} {
		t.Setenv(key, sentinel)
	}
	p, err := mockhost.PrepareScenario(context.Background(), &scenario.Scenario{}, mockhost.PrepareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	env, err := checkEnvironment(p, true, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range env {
		if _, value, _ := strings.Cut(entry, "="); value == sentinel {
			t.Fatal("inherited directory/proxy reached child")
		}
	}
	for _, bad := range []string{"HOME", "XDG_DATA_HOME", "HTTPS_PROXY", "SSH_AUTH_SOCK", "ARVEL_PLUGIN_DATA_DIR"} {
		q, err := mockhost.PrepareScenario(context.Background(), &scenario.Scenario{Plugin: scenario.Plugin{Env: map[string]string{bad: "override"}}}, mockhost.PrepareOptions{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = checkEnvironment(q, true, true)
		_ = q.Close()
		if err == nil {
			t.Fatal("controlled env override accepted")
		}
	}
}

func strictReleaseFiles(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	scenarios := filepath.Join(root, "conformance")
	fake := filepath.Join(root, "testdata", "fakevendor")
	for _, dir := range []string{scenarios, fake} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(fake, "hold.yaml"), "name: hold\nsteps: [{sleepMillis: 120000}]\n")
	write(filepath.Join(fake, "proxy.yaml"), "name: proxy\nsteps:\n - emit: {env: HTTPS_PROXY}\n - emit: {env: HTTP_PROXY}\n - emit: {env: ALL_PROXY}\n - emit: {env: NO_PROXY}\n - emit: {env: SSH_AUTH_SOCK}\n")
	write(filepath.Join(scenarios, "proxy.yaml"), `name: strict-proxy
plugin:
  env: { FIXTURE_MODE: proxy, FIXTURE_START_REQUIRED: yes, FIXTURE_START: prepared }
steps:
 - call: _arvel/status
   expect: { state: ready }
 - call: session/new
   params: { cwd: "{{workspace}}", mcpServers: [] }
   save: { session: $.sessionId }
 - call: session/prompt
   params: { sessionId: "{{session}}", prompt: [{type: text, text: proxy}] }
   expect: {stopReason: end_turn}
`)
	config := filepath.Join(root, "environment.yaml")
	write(config, fmt.Sprintf(`initialize:
  connection:
    kind: synthetic
    config: {executable: "{{fakevendor:proxy}}", enabled: false, count: 2}
    secrets: {token: synthetic-private-token}
plugin:
  env: {FIXTURE_MODE: normal, FIXTURE_AUDIT: %q}
checks:
  concurrency:
    plugin: {env: {FIXTURE_MODE: interleave}}
    prompt: [{type: text, text: overlap}]
  cancel:
    initialize: {connection: {config: {holdExecutable: "{{fakevendor:hold}}"}}}
    plugin: {env: {FIXTURE_MODE: hold}}
    prompt: [{type: text, text: wait}]
  busy:
    initialize: {connection: {config: {holdExecutable: "{{fakevendor:hold}}"}}}
    plugin: {env: {FIXTURE_MODE: hold}}
    prompt: [{type: text, text: wait}]
`, filepath.Join(root, "audit.jsonl")))
	return config, scenarios
}

func TestStrictRelease_Executable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds checker, plugin and fake child executables")
	}
	checker := filepath.Join(t.TempDir(), "checker")
	if runtime.GOOS == "windows" {
		checker += ".exe"
	}
	if b, err := exec.Command("go", "build", "-trimpath", "-o", checker, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check").CombinedOutput(); err != nil {
		t.Fatalf("build trimmed checker: %v (%d output bytes)", err, len(b))
	}
	consumer := t.TempDir()
	sdkRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	gomod := fmt.Sprintf("module synthetic.consumer\n\ngo 1.27.2\n\nrequire github.com/arvel-dev/arvel-sdk v0.0.0\nreplace github.com/arvel-dev/arvel-sdk => %q\n", filepath.ToSlash(sdkRoot))
	if os.WriteFile(filepath.Join(consumer, "go.mod"), []byte(gomod), 0600) != nil {
		t.Fatal("consumer module")
	}
	plugin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/strictprovider", "strict")
	manifestPath, err := filepath.Abs(filepath.Join("testdata", "strictprovider", "arvel.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			cfg, sc := strictReleaseFiles(t)
			args := []string{"--json", "--level", "release", "--test-env", cfg, "--manifest", manifestPath, "--scenarios", sc, "--", plugin}
			cmd := exec.Command(checker, args...)
			cmd.Dir = consumer
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			b, err := cmd.Output()
			if err != nil {
				t.Errorf("strict release failed: %v; report %s; stderr bytes %d", err, b, stderr.Len())
				return
			}
			if bytes.Contains(b, []byte("synthetic-private-token")) {
				t.Error("secret in JSON report")
			}
			var rep Report
			if err := json.Unmarshal(b, &rep); err != nil {
				t.Error(err)
				return
			}
			audit, err := os.ReadFile(filepath.Join(filepath.Dir(cfg), "audit.jsonl"))
			if err != nil {
				t.Error("missing fixture audit")
				return
			}
			roots := map[string]bool{}
			holdChildren := 0
			dependencies := 0
			for _, line := range bytes.Split(bytes.TrimSpace(audit), []byte("\n")) {
				var event struct {
					Event      string `json:"event"`
					DataDir    string `json:"dataDir"`
					CWD        string `json:"cwd"`
					Executable string `json:"executable"`
					Child      int    `json:"child"`
				}
				if json.Unmarshal(line, &event) != nil {
					t.Error("invalid fixture audit")
					continue
				}
				roots[event.DataDir] = true
				for _, path := range []string{event.DataDir, event.CWD, filepath.Dir(event.Executable)} {
					if path == "" {
						continue
					}
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Error("owned test resource remained after process exit")
					}
				}
				if event.Event == "hold" && event.Child > 0 {
					holdChildren++
				}
				if event.Event == "dependency" && event.Child > 0 {
					dependencies++
				}
			}
			if len(roots) < 6 || holdChildren != 2 || dependencies < 6 {
				t.Error("release did not exercise isolated lifecycles and real children")
			}
			for _, name := range []string{"initialize", "concurrency", "cancel", "busy", "scenario proxy.yaml", "proxy"} {
				c := findCheckByName(rep.Checks, name)
				if c == nil || c.Status != StatusPass {
					t.Errorf("strict check %s did not PASS (report statuses: %v)", name, rep.Checks)
				}
			}
		})
	}
	wg.Wait()
}

func TestEnvironment_ConfigurationDiagnostics(t *testing.T) {
	for _, body := range []string{"initialize: {connection: {secrets: {token: [private-value]}}}", "plugin: {env: {X: secret}}\nplugin: {}", "checks: {unknown: {}}", "initialize: {}\n---\n{}"} {
		file := filepath.Join(t.TempDir(), "env.yaml")
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadTestConfiguration(file)
		if err == nil {
			t.Fatal("invalid configuration accepted")
		}
		if strings.Contains(err.Error(), "private-value") {
			t.Fatal("configuration error leaked value")
		}
	}
}

func TestEnvironment_EmptyKindOverlayFails(t *testing.T) {
	for _, body := range []string{"checks: {basic: {initialize: {connection: {kind: ''}}}}", "initialize: {connection: {kind: ''}}"} {
		file := filepath.Join(t.TempDir(), "env.yaml")
		if os.WriteFile(file, []byte(body), 0600) != nil {
			t.Fatal("fixture")
		}
		cfg, err := loadTestConfiguration(file)
		if err != nil {
			t.Fatal(err)
		}
		_, err = prepareCheck(context.Background(), &flags{config: cfg}, "basic", nil, "", "")
		if err == nil || !strings.Contains(err.Error(), "empty kind") {
			t.Fatal("explicit empty kind was lost")
		}
	}
	sc, err := scenario.Load(strings.NewReader("name: empty\ninitialize: {connection: {kind: ''}}\nsteps: [{call: _arvel/status}]"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepareCheck(context.Background(), &flags{}, "scenario", sc, "", ""); err == nil || !strings.Contains(err.Error(), "empty kind") {
		t.Fatal("scenario empty kind lost")
	}
}

func TestEnvironment_NativeEnvNames(t *testing.T) {
	base := testSettings{Plugin: scenario.Plugin{Env: map[string]string{"Fixture": "base"}}}
	over := testSettings{Plugin: scenario.Plugin{Env: map[string]string{"FIXTURE": "override"}}}
	merged := mergeSettings(base, over).Plugin.Env
	if runtime.GOOS == "windows" {
		if len(merged) != 1 || merged["FIXTURE"] != "override" {
			t.Fatal("Windows override priority failed")
		}
		if validateEnvironmentKeys(map[string]string{"Fixture": "one", "FIXTURE": "two"}) == nil {
			t.Fatal("Windows duplicate aliases accepted")
		}
	} else if len(merged) != 2 {
		t.Fatal("POSIX env case changed")
	}
}

func TestEnvironment_ConfigConstraintsAndDefaults(t *testing.T) {
	min, max := int64(1), int64(3)
	m := &manifest.Manifest{Connections: []manifest.Connection{{Kind: "synthetic", Config: manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"count": {Type: "integer", Minimum: &min, Maximum: &max, Default: 2}, "choice": {Type: "string", Enum: []string{"fixture"}, Pattern: "^fixture$", Default: "fixture"}, "enabled": {Type: "boolean", Default: false},
	}}}}}
	meta := protocol.InitMeta{}
	if err := connectionDefaults(m, &meta, false); err != nil {
		t.Fatal(err)
	}
	if meta.Connection.Config["enabled"] != false || meta.Connection.Config["count"] != 2 {
		t.Fatal("typed defaults lost")
	}
	for key, value := range map[string]any{"count": 4, "choice": "not-allowed", "enabled": "false"} {
		bad := protocol.InitMeta{Connection: protocol.ConnectionInfo{Kind: "synthetic", Config: map[string]any{key: value}}}
		if err := connectionDefaults(m, &bad, true); err == nil || !strings.Contains(err.Error(), key) {
			t.Fatal("declared constraint not diagnosed")
		}
	}
}

func TestStrictRelease_NegativeAssertions(t *testing.T) {
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/strictprovider", "strict")
	for _, tc := range []struct{ name, mode, check string }{
		{"early cancel", "normal", "cancel"}, {"early busy", "normal", "busy"},
		{"not ready", "not-ready", "concurrency"}, {"canary still checked", "leak-canary", "cancel"},
		{"announced resume is required", "reject-resume", "concurrency"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, sc := strictReleaseFiles(t)
			parsed, err := loadTestConfiguration(cfg)
			if err != nil {
				t.Fatal(err)
			}
			settings := parsed.Checks[tc.check]
			settings.Plugin.Env = map[string]string{"FIXTURE_MODE": tc.mode}
			settings.Initialize.Connection.Config = nil
			parsed.Checks[tc.check] = settings
			f := &flags{binary: bin, manifest: filepath.Join("testdata", "strictprovider", "arvel.json"), scenarios: sc, config: parsed}
			var got CheckResult
			switch tc.check {
			case "cancel":
				got = checkCancel(context.Background(), f)
			case "busy":
				got = checkBusy(context.Background(), f)
			case "concurrency":
				got = checkConcurrency(context.Background(), f)
			}
			if got.Status != StatusFail {
				t.Fatal("configured assertion did not fail")
			}
			if strings.Contains(got.Detail, "synthetic-private-token") || strings.Contains(got.Detail, "arvel-canary-") {
				t.Fatal("secret appeared in check diagnostic")
			}
			if tc.mode == "leak-canary" && !strings.Contains(got.Detail, "injected") {
				t.Fatal("canary failure was masked")
			}
			if tc.mode == "reject-resume" && !strings.Contains(got.Detail, "announced resume") {
				t.Fatal("resume failure was not exercised")
			}
		})
	}
	t.Run("wrong proxy cannot pass", func(t *testing.T) {
		cfg, sc := strictReleaseFiles(t)
		file := filepath.Join(sc, "proxy.yaml")
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		b = bytes.ReplaceAll(b, []byte("FIXTURE_MODE: proxy"), []byte("FIXTURE_MODE: bad-proxy"))
		if os.WriteFile(file, b, 0600) != nil {
			t.Fatal("write fixture")
		}
		f := &flags{binary: bin, manifest: filepath.Join("testdata", "strictprovider", "arvel.json"), scenarios: sc, testEnv: cfg}
		if c := runProxyCheck(context.Background(), f, sc); c.Status != StatusFail {
			t.Fatal("broken proxy forwarding passed")
		}
	})
}

func TestStrictRelease_ReportsOmitSecrets(t *testing.T) {
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/strictprovider", "strict")
	for _, secret := range []string{"synthetic-private-token", "s3crt!", "quoted-\"synthetic-secret"} {
		for _, jsonReport := range []bool{false, true} {
			t.Run("safe report", func(t *testing.T) {
				cfg, sc := strictReleaseFiles(t)
				raw, err := os.ReadFile(cfg)
				if err != nil {
					t.Fatal(err)
				}
				raw = bytes.ReplaceAll(raw, []byte("synthetic-private-token"), []byte(fmt.Sprintf("%q", secret)))
				raw = bytes.ReplaceAll(raw, []byte("FIXTURE_MODE: normal"), []byte("FIXTURE_MODE: leak"))
				if os.WriteFile(cfg, raw, 0600) != nil {
					t.Fatal("write fixture")
				}
				args := []string{"--test-env", cfg, "--manifest", filepath.Join("testdata", "strictprovider", "arvel.json"), "--scenarios", sc}
				if jsonReport {
					args = append(args, "--json")
				}
				args = append(args, "--", bin)
				var out, stderr bytes.Buffer
				if code := run(context.Background(), args, &out, &stderr); code != 1 {
					t.Fatalf("leaking plugin exit = %d", code)
				}
				if bytes.Contains(out.Bytes(), []byte(secret)) || bytes.Contains(stderr.Bytes(), []byte(secret)) {
					t.Fatal("report exposed configured secret")
				}
			})
		}
	}
}

func TestStrictRelease_RouterUsesDeclaredConnection(t *testing.T) {
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/strictprovider", "strict")
	cfg, sc := strictReleaseFiles(t)
	parsed, err := loadTestConfiguration(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Plugin.Env["FIXTURE_TYPE"] = "router"
	raw, err := os.ReadFile(filepath.Join("testdata", "strictprovider", "arvel.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		t.Fatal("manifest")
	}
	m["type"] = "router"
	m["permissions"] = map[string]any{"network": []string{"127.0.0.1"}}
	m["capabilities"] = map[string]any{"router": map[string]any{"features": []string{"route.plan"}}}
	raw, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "arvel.json")
	if os.WriteFile(file, raw, 0600) != nil {
		t.Fatal("write manifest")
	}
	loaded, err := manifest.LoadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// Provider scenarios are intentionally absent from this router suite.
	empty := filepath.Join(filepath.Dir(sc), "router-conformance")
	if err := os.Mkdir(empty, 0700); err != nil {
		t.Fatal(err)
	}
	f := &flags{binary: bin, manifest: file, scenarios: empty, config: parsed, loaded: loaded, level: "release"}
	rep := runRouter(context.Background(), f, loaded)
	if !rep.ok() {
		t.Fatalf("strict router failed: %v", rep.Checks)
	}
	for _, name := range []string{"initialize", "session/new", "route validity"} {
		c := findCheckByName(rep.Checks, name)
		if c == nil || c.Status != StatusPass {
			t.Fatalf("router check %s did not pass", name)
		}
	}
	if findCheckByName(rep.Checks, "concurrency") != nil {
		t.Fatal("router received provider probes")
	}
}

func TestStrictRelease_CancellationCleansBorrowedContext(t *testing.T) {
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/strictprovider", "strict")
	cfg, sc := strictReleaseFiles(t)
	parsed, err := loadTestConfiguration(cfg)
	if err != nil {
		t.Fatal(err)
	}
	borrowed := t.TempDir()
	if os.Chmod(borrowed, 0700) != nil {
		t.Fatal("private directory")
	}
	sentinel := filepath.Join(borrowed, "sentinel")
	if os.WriteFile(sentinel, []byte("keep"), 0600) != nil {
		t.Fatal("sentinel")
	}
	parsed.Initialize.DataDir = borrowed
	parsed.Workspace = borrowed
	settings := parsed.Checks["cancel"]
	settings.Plugin.Env = map[string]string{"FIXTURE_MODE": "hold"}
	parsed.Checks["cancel"] = settings
	f := &flags{binary: bin, manifest: filepath.Join("testdata", "strictprovider", "arvel.json"), scenarios: sc, config: parsed}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := prepareCheck(ctx, f, "cancel", nil, resolveFakevendorDir(sc), "")
	if err != nil {
		t.Fatal(err)
	}
	root := r.prepared.Environment().Root()
	exe := r.meta.Connection.Config["executable"].(string)
	if _, err = r.initialize(); err != nil {
		t.Fatal(err)
	}
	if err = r.ready(); err != nil {
		t.Fatal(err)
	}
	id, err := r.newSession()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = r.promptSession(id) }()
	if err = r.waitActive(id, done); err != nil {
		t.Fatal(err)
	}
	cancel()
	rows := []CheckResult{}
	r.finish(&rows)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("prompt worker survived cancellation")
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("owned context retained")
	}
	if _, err = os.Stat(filepath.Dir(exe)); !os.IsNotExist(err) {
		t.Fatal("fake retained")
	}
	if _, err = os.Stat(sentinel); err != nil {
		t.Fatal("borrowed directory removed")
	}
}

func TestStrictRelease_ErrorPathsPreserveBorrowedDirectories(t *testing.T) {
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/strictprovider", "strict")
	for _, failure := range []string{"initialize", "scenario"} {
		t.Run(failure, func(t *testing.T) {
			cfg, sc := strictReleaseFiles(t)
			parsed, err := loadTestConfiguration(cfg)
			if err != nil {
				t.Fatal(err)
			}
			borrowed := t.TempDir()
			if os.Chmod(borrowed, 0700) != nil {
				t.Fatal("private dir")
			}
			sentinel := filepath.Join(borrowed, "sentinel")
			if os.WriteFile(sentinel, []byte("keep"), 0600) != nil {
				t.Fatal("sentinel")
			}
			parsed.Initialize.DataDir = borrowed
			parsed.Workspace = borrowed
			if failure == "initialize" {
				parsed.Initialize.Connection.Config["count"] = 3
			} // Valid schema, rejected by strict plugin.
			f := &flags{binary: bin, manifest: filepath.Join("testdata", "strictprovider", "arvel.json"), scenarios: sc, config: parsed}
			scenarioInput := &scenario.Scenario{Name: "failure", Steps: []scenario.Step{{Call: protocol.MethodStatus, Params: map[string]any{"bad": "{{missing}}"}}}}
			r, err := prepareCheck(context.Background(), f, "scenario", scenarioInput, resolveFakevendorDir(sc), "")
			if err != nil {
				t.Fatal(err)
			}
			root := r.prepared.Environment().Root()
			exe := r.meta.Connection.Config["executable"].(string)
			_, err = r.initialize()
			if failure == "initialize" && err == nil {
				t.Fatal("fixture did not reject input")
			}
			if failure == "scenario" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = r.prepared.RunInitialized(context.Background(), r.d); err == nil {
					t.Fatal("bad scenario accepted")
				}
			}
			rows := []CheckResult{}
			r.finish(&rows)
			if _, err = os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("owned root remained after error")
			}
			if _, err = os.Stat(filepath.Dir(exe)); !os.IsNotExist(err) {
				t.Fatal("fake remained after error")
			}
			if _, err = os.Stat(sentinel); err != nil {
				t.Fatal("borrowed data removed on error")
			}
		})
	}
}
