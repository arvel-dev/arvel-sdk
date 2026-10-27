// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/testkit/fakevendor"
	"github.com/arvel-dev/arvel-sdk/testkit/mockhost"
	"github.com/arvel-dev/arvel-sdk/testkit/scenario"
)

// proxyVars is the set of environment variables the proxy check asserts. The
// provider must forward sandbox.proxy into the first three and leave
// SSH_AUTH_SOCK unset (PROTOCOL §15.4).
var proxyVars = []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY", "SSH_AUTH_SOCK"}

// noProxyValue is the standard localhost exclusion set (PROTOCOL §15.3).
const noProxyValue = "localhost,127.0.0.1"

// checkProxyEnv asserts the five proxy environment variables against the
// injected proxy. It returns a list of human-readable violations (empty = pass).
func checkProxyEnv(reported map[string]string, proxy string) []string {
	var out []string
	for _, name := range []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY"} {
		if got := reported[name]; got != proxy {
			out = append(out, fmt.Sprintf("%s=%q, want %q", name, got, proxy))
		}
	}
	if got := reported["NO_PROXY"]; got != noProxyValue {
		out = append(out, fmt.Sprintf("NO_PROXY=%q, want %q", got, noProxyValue))
	}
	if got, ok := reported["SSH_AUTH_SOCK"]; ok && got != "" {
		out = append(out, fmt.Sprintf("SSH_AUTH_SOCK=%q, want absent", got))
	}
	return out
}

// runProxyCheck runs the release-level proxy test when a proxy scenario is
// present, and skips it otherwise (PROTOCOL §15).
func runProxyCheck(ctx context.Context, f *flags, dir string) CheckResult {
	_, sc, err := findProxyScenario(dir)
	if err != nil {
		return fail("proxy", err.Error())
	}
	if sc == nil {
		return skip("proxy", "no proxy scenario")
	}

	tmp, err := os.MkdirTemp("", "arvel-proxy-")
	if err != nil {
		return fail("proxy", err.Error())
	}
	defer os.RemoveAll(tmp)

	fakevendorDir := resolveFakevendorDir(dir)
	env, cleanup, err := resolveProxyEnv(sc, fakevendorDir, tmp)
	if err != nil {
		return fail("proxy", err.Error())
	}
	defer cleanup()

	proxy := randomProxyURL()
	d, err := mockhost.StartDriver(f.binary, mockhost.WithArgs(f.args...), mockhost.WithEnv(append(os.Environ(), env...)))
	if err != nil {
		return fail("proxy", "start: "+err.Error())
	}
	defer d.Close()

	res, runErr := d.RunScenario(sc, mockhost.ScenarioOptions{
		Tmpdir:        tmp,
		FakevendorDir: fakevendorDir,
		Sandbox:       &protocol.SandboxInfo{Mode: protocol.SandboxModeOS, Proxy: proxy},
	})
	if runErr != nil {
		return fail("proxy", runErr.Error())
	}
	if !res.Pass {
		return fail("proxy", firstScenarioFailure(res))
	}
	if violations := checkProxyEnv(collectEnv(res.Updates), proxy); len(violations) > 0 {
		return fail("proxy", strings.Join(violations, "; "))
	}
	return pass("proxy", "vendor reported the proxy environment correctly")
}

// findProxyScenario locates the proxy scenario: a file named proxy.yaml, else
// the first scenario whose plugin.env references a fakevendor placeholder. A
// missing directory yields no scenario, not an error.
func findProxyScenario(dir string) (string, *scenario.Scenario, error) {
	if p := filepath.Join(dir, "proxy.yaml"); fileExists(p) {
		sc, err := scenario.LoadFile(p)
		if err != nil {
			return "", nil, err
		}
		return p, sc, nil
	}
	files, err := scenarioFiles(dir)
	if err != nil {
		return "", nil, nil
	}
	for _, f := range files {
		sc, err := scenario.LoadFile(f)
		if err != nil {
			return "", nil, err
		}
		for _, v := range sc.Plugin.Env {
			if strings.Contains(v, "{{fakevendor:") {
				return f, sc, nil
			}
		}
	}
	return "", nil, nil
}

// resolveProxyEnv resolves the scenario's plugin.env placeholders into
// NAME=VALUE strings and returns a cleanup that removes any built fakevendor
// binaries.
func resolveProxyEnv(sc *scenario.Scenario, fakevendorDir, tmpdir string) ([]string, func(), error) {
	var cleanups []func()
	resolver := scenario.Resolver{
		Tmpdir: tmpdir,
		Fakevendor: func(name string) (string, error) {
			script := filepath.Join(fakevendorDir, name+".yaml")
			s, err := fakevendor.LoadFile(script)
			if err != nil {
				return "", err
			}
			bin, cleanup, err := fakevendor.Build(context.Background(), s, "fakevendor-"+name)
			if err != nil {
				return "", err
			}
			cleanups = append(cleanups, cleanup)
			return bin, nil
		},
	}
	env := make([]string, 0, len(sc.Plugin.Env))
	for k, v := range sc.Plugin.Env {
		rv, err := resolver.ResolveString(v)
		if err != nil {
			return nil, nil, fmt.Errorf("proxy: plugin.env %s: %w", k, err)
		}
		env = append(env, k+"="+rv)
	}
	sort.Strings(env)
	return env, func() {
		for _, c := range cleanups {
			c()
		}
	}, nil
}

// collectEnv scans every collected session update for agent_message_chunk text
// of the form NAME=VALUE and returns the reported environment.
func collectEnv(updates map[string][]map[string]any) map[string]string {
	reported := map[string]string{}
	for _, ups := range updates {
		for _, u := range ups {
			if u["sessionUpdate"] != "agent_message_chunk" {
				continue
			}
			content, ok := u["content"].(map[string]any)
			if !ok {
				continue
			}
			text, _ := content["text"].(string)
			for _, line := range strings.Split(text, "\n") {
				k, v, ok := strings.Cut(line, "=")
				if ok && k != "" {
					reported[k] = v
				}
			}
		}
	}
	return reported
}

// randomProxyURL returns a fresh loopback http URL for the injected proxy.
func randomProxyURL() string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	port := 20000 + (int(b[0])<<8|int(b[1]))%40000
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// fileExists reports whether path names a regular file.
func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
