// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/manifest"
	"github.com/arvel-dev/arvel-sdk/redact"
)

func buildBinary(t *testing.T, pkg, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", out, pkg)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, b)
	}
	return out
}

// runBasic builds the manifest/flag inputs and runs the provider basic suite.
func runBasic(t *testing.T, bin, manifestPath string) []CheckResult {
	t.Helper()
	m, err := manifest.LoadFile(manifestPath)
	if err != nil {
		t.Fatalf("load manifest %s: %v", manifestPath, err)
	}
	f := &flags{level: "basic", binary: bin, manifest: manifestPath}
	return providerBasic(context.Background(), f, m)
}

func findCheck(t *testing.T, cs []CheckResult, name string) CheckResult {
	t.Helper()
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found", name)
	return CheckResult{}
}

func TestProviderBasic_EchoPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the example binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/echo", "echo")
	cs := runBasic(t, bin, filepath.Join("..", "..", "examples", "echo", "arvel.json"))

	for _, name := range []string{"initialize", "status", "models", "unknown method", "shutdown", "canary leak"} {
		if c := findCheck(t, cs, name); c.Status != StatusPass {
			t.Fatalf("check %q: status=%s detail=%s", name, c.Status, c.Detail)
		}
	}
	// stderr is empty for echo, so the JSON-lines check reports skip, never fail.
	if c := findCheck(t, cs, "stderr JSON lines"); c.Status == StatusFail {
		t.Fatalf("stderr check failed: %s", c.Detail)
	}
}

func TestProviderBasic_StubFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub binary")
	}
	cases := []struct {
		mode   string
		check  string
		detail string
	}{
		// The stub is built on the SDK, so the plugin framework validates the
		// models result before it reaches the wire and answers a generic -32000
		// "invalid models result" error; the conformance CLI reports that.
		{"duplicate-model", "models", "invalid models result"},
		{"unknown-effort", "models", "invalid models result"},
		{"echo-canary", "canary leak", "appeared in plugin output"},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			t.Setenv("STUB_MODE", c.mode)
			bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/stubprovider", "stub")
			cs := runBasic(t, bin, filepath.Join("testdata", "stubprovider", "arvel.json"))

			got := findCheck(t, cs, c.check)
			if got.Status != StatusFail {
				t.Fatalf("check %q: status=%s, want fail (detail=%s)", c.check, got.Status, got.Detail)
			}
			if !strings.Contains(got.Detail, c.detail) {
				t.Fatalf("check %q detail %q does not contain %q", c.check, got.Detail, c.detail)
			}
			// No detail may re-print the injected canary.
			for _, res := range cs {
				if strings.Contains(res.Detail, "arvel-canary-") {
					t.Fatalf("detail %q leaks the canary", res.Detail)
				}
			}
		})
	}
}

func TestProviderBasic_ShutdownTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub binary")
	}
	t.Setenv("STUB_MODE", "ignore-shutdown")
	old := shutdownTimeout
	shutdownTimeout = 500 * time.Millisecond
	t.Cleanup(func() { shutdownTimeout = old })

	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/stubprovider", "stub")
	cs := runBasic(t, bin, filepath.Join("testdata", "stubprovider", "arvel.json"))

	if c := findCheck(t, cs, "shutdown"); c.Status != StatusFail {
		t.Fatalf("shutdown: status=%s, want fail (detail=%s)", c.Status, c.Detail)
	}
}

// The redactor keeps a canary out of a detail even when a plugin echoes it into
// a non-JSON stderr line that another check reports.
func TestRedactDetails(t *testing.T) {
	cs := []CheckResult{fail("stderr JSON lines", "line 1 is not JSON: leaked arvel-canary-0123456789abcdef")}
	redactDetails(cs, "arvel-canary-0123456789abcdef")
	if got := cs[0].Detail; strings.Contains(got, "arvel-canary-") || !strings.Contains(got, redact.Replacement) {
		t.Fatalf("detail %q was not redacted", got)
	}
}
