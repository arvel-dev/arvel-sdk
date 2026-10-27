// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunScenarios_EchoPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the example binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/echo", "echo")
	f := &flags{binary: bin}
	cs := runScenarios(context.Background(), f, filepath.Join("..", "..", "examples", "echo", "conformance"))

	for _, name := range []string{"scenario basic-turn.yaml", "scenario permission.yaml"} {
		if c := findCheck(t, cs, name); c.Status != StatusPass {
			t.Fatalf("check %q: status=%s detail=%s", name, c.Status, c.Detail)
		}
	}

	// Each of the 4 basic-turn steps emits its own passing row.
	steps := 0
	for _, c := range cs {
		if strings.HasPrefix(c.Name, "scenario basic-turn.yaml · step ") {
			steps++
			if c.Status != StatusPass {
				t.Fatalf("step %q: status=%s detail=%s", c.Name, c.Status, c.Detail)
			}
		}
	}
	if steps != 4 {
		t.Fatalf("basic-turn step rows = %d, want 4", steps)
	}
}

func TestRunScenarios_WrongExpectFails(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the example binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/echo", "echo")
	dir := t.TempDir()
	sc := "name: wrong\nplugin: {}\ninitialize:\n  connection: { kind: local, config: {} }\nsteps:\n  - call: _arvel/status\n    expect: { state: busy }\n"
	if err := os.WriteFile(filepath.Join(dir, "wrong.yaml"), []byte(sc), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &flags{binary: bin}
	cs := runScenarios(context.Background(), f, dir)

	if c := findCheck(t, cs, "scenario wrong.yaml"); c.Status != StatusFail {
		t.Fatalf("status=%s, want fail (detail=%s)", c.Status, c.Detail)
	}
}

func TestRunScenarios_EmptyDirSkips(t *testing.T) {
	dir := t.TempDir()
	f := &flags{binary: "unused"}
	cs := runScenarios(context.Background(), f, dir)
	if len(cs) != 1 || cs[0].Name != "scenarios" || cs[0].Status != StatusSkip {
		t.Fatalf("cs = %+v, want a single skip row", cs)
	}
}
