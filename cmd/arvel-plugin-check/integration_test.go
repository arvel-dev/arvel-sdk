// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// repoPath resolves a path from the repository root (the tests run with the
// package directory as the working directory).
func repoPath(parts ...string) string {
	return filepath.Join("..", "..", filepath.Join(parts...))
}

// runJSON drives the CLI's run entrypoint with --json and returns the decoded
// report, failing the test when the exit code is not want.
func runJSON(t *testing.T, want int, args ...string) *Report {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := run(context.Background(), append([]string{"--json"}, args...), &out, &errBuf)
	if code != want {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, want, out.String(), errBuf.String())
	}
	var rep Report
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("decode --json report: %v\n%s", err, out.String())
	}
	if rep.Passed+rep.Failed+rep.Skipped != len(rep.Checks) {
		t.Fatalf("tallies %d+%d+%d do not add up to %d checks",
			rep.Passed, rep.Failed, rep.Skipped, len(rep.Checks))
	}
	return &rep
}

// TestEchoRelease is the S5 DoD: examples/echo passes the release level,
// including the scenario runs.
func TestEchoRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the example binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/echo", "echo")
	rep := runJSON(t, 0,
		"--level", "release",
		"--manifest", repoPath("examples", "echo", "arvel.json"),
		"--scenarios", repoPath("examples", "echo", "conformance"),
		"--", bin)
	if rep.Kind != "provider" || rep.Level != "release" {
		t.Fatalf("kind=%q level=%q, want provider/release", rep.Kind, rep.Level)
	}
	if rep.Failed != 0 {
		t.Fatalf("report has %d failed checks", rep.Failed)
	}
	for _, name := range []string{"manifest", "initialize", "status", "models", "unknown method", "shutdown", "canary leak"} {
		if c := findCheckByName(rep.Checks, name); c == nil || c.Status != StatusPass {
			t.Fatalf("check %q: %+v, want pass", name, c)
		}
	}
	for _, name := range []string{"scenario basic-turn.yaml", "scenario permission.yaml"} {
		if c := findCheckByName(rep.Checks, name); c == nil || c.Status == StatusFail {
			t.Fatalf("check %q: %+v, want pass or skip", name, c)
		}
	}
}

// TestEchoRouterRelease is the S5 DoD: examples/echo-router passes the release
// level with the router suite.
func TestEchoRouterRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the example binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/echo-router", "echo-router")
	rep := runJSON(t, 0,
		"--level", "release",
		"--manifest", repoPath("examples", "echo-router", "arvel.json"),
		"--scenarios", repoPath("examples", "echo-router", "conformance"),
		"--", bin)
	if rep.Failed != 0 {
		t.Fatalf("report has %d failed checks", rep.Failed)
	}
}

// TestTeamSampleManifest is the S5 DoD: examples/team-sample passes the
// declarative --manifest mode.
func TestTeamSampleManifest(t *testing.T) {
	if testing.Short() {
		t.Skip("manifest and file validation of the example")
	}
	rep := runJSON(t, 0,
		"--level", "basic",
		"--manifest", repoPath("examples", "team-sample", "arvel.json"),
		"--package", repoPath("examples", "team-sample"))
	for _, name := range []string{"manifest", "package files"} {
		if c := findCheckByName(rep.Checks, name); c == nil || c.Status != StatusPass {
			t.Fatalf("check %q: %+v, want pass", name, c)
		}
	}
}

// Exercise the documented ./bin/plugin command shape rather than only passing
// absolute paths returned by the test build helper.
func TestRun_RelativeBinariesPassRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("builds example binaries")
	}
	for _, kind := range []string{"echo", "echo-router"} {
		t.Run(kind, func(t *testing.T) {
			bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/"+kind, kind)
			manifestPath, err := filepath.Abs(repoPath("examples", kind, "arvel.json"))
			if err != nil {
				t.Fatal(err)
			}
			scenarios, err := filepath.Abs(repoPath("examples", kind, "conformance"))
			if err != nil {
				t.Fatal(err)
			}
			t.Chdir(filepath.Dir(bin))
			if _, err := os.Stat(filepath.Base(bin)); err != nil {
				t.Fatal(err)
			}
			runJSON(t, 0, "--level", "release", "--manifest", manifestPath, "--scenarios", scenarios, "--", "."+string(filepath.Separator)+filepath.Base(bin))
		})
	}
}

func findCheckByName(cs []CheckResult, name string) *CheckResult {
	for i := range cs {
		if cs[i].Name == name {
			return &cs[i]
		}
	}
	return nil
}
