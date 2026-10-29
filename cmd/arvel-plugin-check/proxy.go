// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/arvel-dev/arvel-sdk/testkit/mockhost"
	"github.com/arvel-dev/arvel-sdk/testkit/scenario"
)

// noProxyValue is the standard localhost exclusion set (PROTOCOL §15.3).
const noProxyValue = "localhost,127.0.0.1"

// checkProxyEnv asserts the five proxy environment variables against the
// injected proxy. It returns a list of human-readable violations (empty = pass).
func checkProxyEnv(reported map[string]string, proxy string) []string {
	var out []string
	for _, name := range []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY"} {
		if got := reported[name]; got != proxy {
			out = append(out, fmt.Sprintf("%s: proxy mismatch", name))
		}
	}
	if got := reported["NO_PROXY"]; got != noProxyValue {
		out = append(out, "NO_PROXY: localhost exclusions mismatch")
	}
	if got, ok := reported["SSH_AUTH_SOCK"]; ok && got != "" {
		out = append(out, "SSH_AUTH_SOCK: must be absent")
	}
	return out
}

// runProxyCheck runs the release-level proxy test when a proxy scenario is
// present, and skips it otherwise (PROTOCOL §15).
func runProxyCheck(ctx context.Context, f *flags, dir string) (result CheckResult) {
	_, sc, err := findProxyScenario(dir)
	if err != nil {
		return fail("proxy", err.Error())
	}
	if sc == nil {
		return skip("proxy", "no proxy scenario")
	}

	proxy := randomProxyURL()
	r, err := prepareCheck(ctx, f, "proxy", sc, resolveFakevendorDir(dir), proxy)
	if err != nil {
		return fail("proxy", safeError(err))
	}
	defer r.finishOne(&result)
	var res mockhost.ScenarioResult
	_, runErr := r.initialize()
	if runErr == nil {
		runErr = r.ready()
	}
	if runErr == nil {
		res, runErr = r.prepared.RunInitialized(ctx, r.d)
	}

	if runErr != nil {
		return fail("proxy", safeError(runErr))
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
					if previous, exists := reported[k]; exists && previous != v {
						reported[k] = "conflicting reports"
					} else {
						reported[k] = v
					}
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
