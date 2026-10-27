// SPDX-License-Identifier: MIT-0

package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/arvel-dev/arvel-sdk/testkit/mockhost"
)

func TestEchoScenarios(t *testing.T) {
	for _, f := range []string{"conformance/basic-turn.yaml", "conformance/permission.yaml"} {
		t.Run(f, func(t *testing.T) {
			h := mockhost.InProcess(t, newProvider())
			h.RunScenario(t, f)
			h.Shutdown()
		})
	}
}

func TestEchoScenariosBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the example binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/echo", "echo")
	for _, f := range []string{"conformance/basic-turn.yaml", "conformance/permission.yaml"} {
		t.Run(f, func(t *testing.T) {
			h := mockhost.Start(t, bin)
			h.RunScenario(t, f)
			h.Shutdown()
		})
	}
}

// buildBinary compiles a package into a temporary executable and returns its path.
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
