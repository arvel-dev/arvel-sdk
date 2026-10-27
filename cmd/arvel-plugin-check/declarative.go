// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/arvel-dev/arvel-sdk/manifest"
)

// runDeclarative runs the declarative (team/integration) checks of SPEC §5.10:
// load the manifest, validate it and the files it references in the package
// directory, and — at release for an integration plugin — list the tools of
// each stdio MCP server. No plugin binary is required for a team plugin.
func runDeclarative(ctx context.Context, f *flags) *Report {
	rep := &Report{Plugin: f.manifest, Kind: "", Level: f.level}

	m, err := manifest.LoadFile(f.manifest)
	if err != nil {
		rep.add(fail("manifest load", err.Error()))
		return rep
	}
	rep.Kind = string(m.Type)

	if !m.Type.Declarative() {
		rep.add(fail("manifest", fmt.Sprintf("declarative mode requires type team or integration, got %q", m.Type)))
		return rep
	}

	// The manifest is the gate: without a valid manifest the contributes block
	// cannot be trusted, so the file and MCP checks are skipped.
	rep.add(checkManifest(f.manifest))
	if rep.Failed > 0 {
		return rep
	}

	rep.add(checkPackageFiles(m, f.pkg))
	for _, c := range checkMCPServers(ctx, f, m) {
		rep.add(c)
	}
	return rep
}

// hasFiles reports whether the manifest references at least one instruction or
// skill file inside the package (PROTOCOL §16.2, §16.4).
func hasFiles(m *manifest.Manifest) bool {
	c := m.Contributes
	if c == nil {
		return false
	}
	for _, r := range c.Roles {
		if len(r.Instructions) > 0 {
			return true
		}
	}
	return len(c.Skills) > 0
}

// checkPackageFiles validates the instruction and skill files the manifest
// points at, in the unpacked package directory. Every problem is listed in
// Detail (path, code and message), so a missing instruction file names
// "file-missing".
func checkPackageFiles(m *manifest.Manifest, dir string) CheckResult {
	if dir == "" {
		if hasFiles(m) {
			return fail("package files", "the manifest references instruction or skill files but no --package directory was given")
		}
		return skip("package files", "no --package directory and the manifest references no files")
	}
	ps := m.ValidateFiles(dir)
	if len(ps) == 0 {
		return pass("package files", "all referenced files are present")
	}
	var msgs []string
	for _, p := range ps {
		msgs = append(msgs, p.String())
	}
	return fail("package files", strings.Join(msgs, "; "))
}

// checkMCPServers runs the MCP tool-listing checks of an integration plugin.
// Team plugins have no MCP servers (validated earlier) and produce no checks.
func checkMCPServers(ctx context.Context, f *flags, m *manifest.Manifest) []CheckResult {
	if m.Type != manifest.TypeIntegration || m.Contributes == nil {
		return nil
	}
	// {bin} fallback: the built plugin binary in the package directory, named
	// after the manifest id (the sibling of arvel.json).
	selfBin := filepath.Join(f.pkg, m.ID)
	if runtime.GOOS == "windows" {
		selfBin += ".exe"
	}
	var out []CheckResult
	for _, s := range m.Contributes.MCPServers {
		out = append(out, checkOneMCPServer(ctx, f, selfBin, s))
	}
	return out
}

// checkOneMCPServer checks one MCP server of an integration plugin. Remote (URL)
// servers are not started; stdio servers are started and their tools listed at
// release level only.
func checkOneMCPServer(ctx context.Context, f *flags, selfBin string, s manifest.MCPServerSpec) CheckResult {
	name := "mcp " + s.Name + " tools"
	if f.level != "release" {
		return skip(name, "--level basic does not start MCP servers")
	}
	if s.URL != "" {
		return skip(name, "remote MCP server; tool listing not performed")
	}
	if len(s.Command) == 0 {
		return skip(name, "no stdio command")
	}

	cmd := append([]string(nil), s.Command...)
	if cmd[0] == "{bin}" {
		switch {
		case f.binary != "":
			cmd[0] = f.binary
		default:
			if st, err := os.Stat(selfBin); err == nil && !st.IsDir() {
				cmd[0] = selfBin
			} else {
				return skip(name, "cannot resolve {bin}: no plugin binary (pass it after -- or --package with the built plugin)")
			}
		}
	}

	tools, err := listTools(ctx, cmd, f.pkg)
	if err != nil {
		return fail(name, err.Error())
	}
	detail := fmt.Sprintf("%d tool(s)", len(tools))
	if len(tools) > 0 {
		detail += ": " + strings.Join(tools, ", ")
	}
	// The manifest's Tools list is a documentation hint, not a contract; report
	// declared-but-undiscovered tools without failing.
	if missing := declaredNotDiscovered(s.Tools, tools); len(missing) > 0 {
		detail += fmt.Sprintf("; declared but not discovered: %s", strings.Join(missing, ", "))
	}
	return pass(name, detail)
}

// declaredNotDiscovered returns the declared tool names absent from discovered.
func declaredNotDiscovered(declared, discovered []string) []string {
	set := make(map[string]bool, len(discovered))
	for _, d := range discovered {
		set[d] = true
	}
	var out []string
	for _, d := range declared {
		if !set[d] {
			out = append(out, d)
		}
	}
	return out
}
