// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// declarativeFixtureDir returns the path of a fixture package under testdata.
func declarativeFixtureDir(t *testing.T, name string) (manifestPath, dir string) {
	t.Helper()
	dir = filepath.Join("testdata", "declarative", name)
	return filepath.Join(dir, "arvel.json"), dir
}

func TestDeclarative_TeamSamplePasses(t *testing.T) {
	if testing.Short() {
		t.Skip("manifest and file validation of the example")
	}
	f := &flags{
		level:    "basic",
		manifest: filepath.Join("..", "..", "examples", "team-sample", "arvel.json"),
		pkg:      filepath.Join("..", "..", "examples", "team-sample"),
	}
	rep := runDeclarative(t.Context(), f)
	if rep.Failed != 0 {
		t.Fatalf("report has failures: %+v", rep.Checks)
	}
	for _, name := range []string{"manifest", "package files"} {
		if c := findReportCheck(t, rep, name); c.Status != StatusPass {
			t.Fatalf("check %q: status=%s detail=%s", name, c.Status, c.Detail)
		}
	}
}

func TestDeclarative_MissingInstructionFile(t *testing.T) {
	manifestPath, dir := declarativeFixtureDir(t, "team-missing-file")
	f := &flags{level: "basic", manifest: manifestPath, pkg: dir}
	rep := runDeclarative(t.Context(), f)
	c := findReportCheck(t, rep, "package files")
	if c.Status != StatusFail {
		t.Fatalf("package files: status=%s, want fail", c.Status)
	}
	if !strings.Contains(c.Detail, "file-missing") {
		t.Fatalf("detail %q does not name file-missing", c.Detail)
	}
	if rep.ok() {
		t.Fatal("report must not be ok")
	}
}

func TestDeclarative_RejectsProviderManifest(t *testing.T) {
	manifestPath, dir := declarativeFixtureDir(t, "provider")
	f := &flags{level: "basic", manifest: manifestPath, pkg: dir}
	rep := runDeclarative(t.Context(), f)
	c := findReportCheck(t, rep, "manifest")
	if c.Status != StatusFail {
		t.Fatalf("manifest: status=%s, want fail", c.Status)
	}
	if !strings.Contains(c.Detail, "requires type team or integration") {
		t.Fatalf("detail %q does not name the type requirement", c.Detail)
	}
}

func TestDeclarative_TeamMCPRejected(t *testing.T) {
	manifestPath, dir := declarativeFixtureDir(t, "team-mcp")
	f := &flags{level: "basic", manifest: manifestPath, pkg: dir}
	rep := runDeclarative(t.Context(), f)
	c := findReportCheck(t, rep, "manifest")
	if c.Status != StatusFail {
		t.Fatalf("manifest: status=%s, want fail", c.Status)
	}
	if !strings.Contains(c.Detail, "team-mcp") {
		t.Fatalf("detail %q does not name the team-mcp rule", c.Detail)
	}
}

// At release, the stdio MCP server of an integration plugin is started and its
// tools listed; at basic it is skipped.
func TestDeclarative_MCPServer(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/mcpstub", "mcpstub")
	manifestPath, dir := declarativeFixtureDir(t, "integration-mcp")

	basic := runDeclarative(t.Context(), &flags{level: "basic", manifest: manifestPath, pkg: dir, binary: bin})
	if c := findReportCheck(t, basic, "mcp stub tools"); c.Status != StatusSkip {
		t.Fatalf("basic: mcp stub tools: status=%s, want skip", c.Status)
	}

	rel := runDeclarative(t.Context(), &flags{level: "release", manifest: manifestPath, pkg: dir, binary: bin})
	c := findReportCheck(t, rel, "mcp stub tools")
	if c.Status != StatusPass {
		t.Fatalf("release: mcp stub tools: status=%s detail=%s", c.Status, c.Detail)
	}
	if !strings.Contains(c.Detail, "alpha") || !strings.Contains(c.Detail, "beta") {
		t.Fatalf("detail %q does not list the discovered tools", c.Detail)
	}
	if !strings.Contains(c.Detail, "2 tool(s)") {
		t.Fatalf("detail %q does not report the tool count", c.Detail)
	}
}

// When no binary is given after --, {bin} resolves to the manifest-id-named
// binary sitting in the --package directory.
func TestDeclarative_BinFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/mcpstub", "mcpstub")
	manifestPath, _ := declarativeFixtureDir(t, "integration-mcp")

	pkg := t.TempDir()
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read fixture manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "arvel.json"), raw, 0o644); err != nil {
		t.Fatalf("copy manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkg, filepath.Base(bin)), mustRead(t, bin), 0o755); err != nil {
		t.Fatalf("copy binary: %v", err)
	}

	rep := runDeclarative(t.Context(), &flags{level: "release", manifest: filepath.Join(pkg, "arvel.json"), pkg: pkg})
	c := findReportCheck(t, rep, "mcp stub tools")
	if c.Status != StatusPass {
		t.Fatalf("mcp stub tools: status=%s detail=%s", c.Status, c.Detail)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func findReportCheck(t *testing.T, rep *Report, name string) CheckResult {
	t.Helper()
	for _, c := range rep.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found in report: %+v", name, rep.Checks)
	return CheckResult{}
}
