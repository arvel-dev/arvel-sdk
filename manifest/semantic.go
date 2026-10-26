// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

type problems []Problem

func (ps *problems) add(sev Severity, path, code, format string, args ...any) {
	*ps = append(*ps, Problem{Severity: sev, Path: path, Code: code, Message: fmt.Sprintf(format, args...)})
}

func (ps *problems) err(path, code, format string, args ...any) {
	ps.add(SeverityError, path, code, format, args...)
}

func (ps *problems) warn(path, code, format string, args ...any) {
	ps.add(SeverityWarning, path, code, format, args...)
}

var routerFeatures = []string{"route.plan", "route.message", "route.effort", "route.explain"}

func (m *Manifest) semanticProblems(opts Options) []Problem {
	var ps problems
	m.checkTool(&ps)
	m.checkVersions(&ps)
	m.checkEntry(&ps, opts)
	m.checkRequires(&ps)
	m.checkConnections(&ps)
	if f := m.Permissions.Files; f != "" && f != "session-cwd" {
		ps.warn("/permissions/files", "files-unknown", "unknown files scope %q; only %q is defined", f, "session-cwd")
	}
	m.checkType(&ps)
	m.checkSandbox(&ps)
	return ps
}

func (m *Manifest) checkVersions(ps *problems) {
	if m.Version != "" && !semver.IsValid("v"+m.Version) {
		ps.err("/version", "semver", "%q is not a semantic version", m.Version)
	}
	for i, r := range m.Requires {
		if r.MinVersion != "" && !semver.IsValid("v"+r.MinVersion) {
			ps.err(fmt.Sprintf("/requires/%d/minVersion", i), "semver", "%q is not a semantic version", r.MinVersion)
		}
	}
}

func (m *Manifest) checkEntry(ps *problems, opts Options) {
	if m.Entry.IsCommand() {
		first := m.Entry.Command[0]
		if !slices.Contains(m.Permissions.Spawn, first) {
			ps.err("/entry/command/0", "command-not-in-spawn", "%q must be listed in permissions.spawn", first)
		}
		if !slices.ContainsFunc(m.Requires, func(r Requirement) bool { return r.Binary == first }) {
			ps.err("/entry/command/0", "command-not-in-requires", "%q must be listed in requires", first)
		}
		return
	}
	if len(m.Entry.Platforms) == 0 {
		return // the schema reports an empty entry
	}
	sev := SeverityWarning
	if opts.Official {
		sev = SeverityError
	}
	for _, p := range Platforms() {
		if _, ok := m.Entry.Platforms[p]; !ok {
			ps.add(sev, "/entry/"+p, "platform-missing", "platform %s is not covered", p)
		}
	}
	keys := make([]string, 0, len(m.Entry.Platforms))
	for k := range m.Entry.Platforms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !slices.Contains(Platforms(), k) {
			ps.warn("/entry/"+k, "platform-unknown", "%s is not a platform plugins must support", k)
		}
	}
	if m.Checksums == "" {
		ps.err("/checksums", "checksums-missing", "release assets need a checksums file")
	}
}

func (m *Manifest) checkRequires(ps *problems) {
	for i, r := range m.Requires {
		if r.Binary != "" && !slices.Contains(m.Permissions.Spawn, r.Binary) {
			ps.err(fmt.Sprintf("/requires/%d/binary", i), "spawn-missing", "%q must be listed in permissions.spawn", r.Binary)
		}
	}
}

func (m *Manifest) checkConnections(ps *problems) {
	kinds := map[string]bool{}
	for i, c := range m.Connections {
		base := fmt.Sprintf("/connections/%d", i)
		if c.Kind == "" || kinds[c.Kind] {
			ps.err(base+"/kind", "kind-duplicate", "kind %q is empty or used twice", c.Kind)
		}
		kinds[c.Kind] = true
		if !c.Billing.Valid() {
			ps.err(base+"/billing", "billing", "billing %q is not valid", c.Billing)
		}
		secrets := map[string]bool{}
		for j, s := range c.Secrets {
			sp := fmt.Sprintf("%s/secrets/%d", base, j)
			if s.ID == "" || secrets[s.ID] {
				ps.err(sp+"/id", "secret-duplicate", "secret id %q is empty or used twice", s.ID)
			}
			secrets[s.ID] = true
			if s.Pattern != "" {
				if _, err := regexp.Compile(s.Pattern); err != nil {
					ps.err(sp+"/pattern", "secret-pattern", "pattern does not compile: %v", err)
				}
			}
		}
		checkConfig(ps, base+"/config", c.Config)
	}
}

func checkConfig(ps *problems, base string, cfg ConfigSchema) {
	if cfg.Type != "" && cfg.Type != "object" {
		ps.err(base+"/type", "config-subset", "config type must be %q", "object")
	}
	names := make([]string, 0, len(cfg.Properties))
	for n := range cfg.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		checkProperty(ps, base+"/properties/"+n, n, cfg.Properties[n])
	}
}

func checkProperty(ps *problems, p, name string, prop ConfigProperty) {
	bad := func(format string, args ...any) { ps.err(p, "config-subset", format, args...) }
	if name == "" {
		bad("property name is empty")
	}
	switch prop.Type {
	case "boolean":
		if prop.Default != nil {
			if _, ok := prop.Default.(bool); !ok {
				bad("default must be a boolean")
			}
		}
		if len(prop.Enum) > 0 || prop.Pattern != "" || prop.Minimum != nil || prop.Maximum != nil {
			bad("boolean properties take no enum, pattern, minimum or maximum")
		}
	case "string":
		checkStringProperty(bad, prop)
	case "integer":
		checkIntegerProperty(bad, prop)
	default:
		bad("type %q is outside the allowed subset (boolean, string, integer)", prop.Type)
	}
}

func checkStringProperty(bad func(string, ...any), prop ConfigProperty) {
	if prop.Minimum != nil || prop.Maximum != nil {
		bad("minimum and maximum apply to integers only")
	}
	seen := map[string]bool{}
	for _, e := range prop.Enum {
		if seen[e] {
			bad("enum value %q is repeated", e)
		}
		seen[e] = true
	}
	if prop.Pattern != "" {
		if _, err := regexp.Compile(prop.Pattern); err != nil {
			bad("pattern does not compile: %v", err)
		}
	}
	if prop.Default == nil {
		return
	}
	s, ok := prop.Default.(string)
	if !ok {
		bad("default must be a string")
		return
	}
	if len(prop.Enum) > 0 && !slices.Contains(prop.Enum, s) {
		bad("default %q is not in enum", s)
	}
}

func checkIntegerProperty(bad func(string, ...any), prop ConfigProperty) {
	if len(prop.Enum) > 0 || prop.Pattern != "" {
		bad("enum and pattern apply to strings only")
	}
	if prop.Minimum != nil && prop.Maximum != nil && *prop.Minimum > *prop.Maximum {
		bad("minimum is greater than maximum")
	}
	if prop.Default == nil {
		return
	}
	f, ok := prop.Default.(float64)
	if !ok || f != math.Trunc(f) {
		bad("default must be an integer")
		return
	}
	if prop.Minimum != nil && f < float64(*prop.Minimum) || prop.Maximum != nil && f > float64(*prop.Maximum) {
		bad("default is outside minimum and maximum")
	}
}

// checkTool rejects the old "tool" type with a hint (PROTOCOL §3.2).
func (m *Manifest) checkTool(ps *problems) {
	if string(m.Type) == "tool" {
		ps.err("/type", "type-tool", "type \"tool\" was replaced by \"integration\": declare an integration plugin")
	}
}

func (m *Manifest) checkType(ps *problems) {
	if m.Type != TypeRouter && m.Capabilities.Router != nil {
		ps.warn("/capabilities/router", "provider-router-caps", "router capabilities on a provider are ignored")
	}
	if m.Type != TypeRouter && !m.Type.Declarative() && m.Contributes != nil {
		ps.warn("/contributes", "contributes-ignored", "contributes is used by team and integration plugins only")
	}
	switch {
	case m.Type == TypeRouter:
		m.checkRouter(ps)
	case m.Type.Declarative():
		m.checkDeclarative(ps)
	}
}

func (m *Manifest) checkRouter(ps *problems) {
	r := m.Capabilities.Router
	if r == nil || len(r.Features) == 0 {
		ps.err("/capabilities/router/features", "router-features", "a router must list at least one route.* feature")
	} else {
		for i, f := range r.Features {
			if !slices.Contains(routerFeatures, f) {
				ps.err(fmt.Sprintf("/capabilities/router/features/%d", i), "router-features", "%q is not a router feature", f)
			}
		}
	}
	if len(m.Connections) == 0 {
		ps.err("/connections", "router-connection", "a router needs at least one connection")
	}
	if len(m.Permissions.Network) == 0 && slices.ContainsFunc(m.Connections, func(c Connection) bool { return len(c.Secrets) > 0 }) {
		ps.err("/permissions/network", "router-network", "a router with secrets contacts a remote service: list its hosts in permissions.network")
	}
	if m.Sandbox != nil {
		ps.err("/sandbox", "router-sandbox", "routers have no sandbox block")
	}
	if len(m.Permissions.Spawn) > 0 {
		ps.warn("/permissions/spawn", "router-spawn", "a router should not spawn programs")
	}
}

func (m *Manifest) checkSandbox(ps *problems) {
	if m.Sandbox == nil || m.Type == TypeRouter {
		return
	}
	if m.Type.Declarative() {
		ps.err("/sandbox", "declarative-sandbox", "%s plugins have no sandbox block", m.Type)
		return
	}
	for _, kind := range []struct {
		name  string
		paths []string
	}{{"read", m.Sandbox.VendorPaths.Read}, {"write", m.Sandbox.VendorPaths.Write}} {
		for i, p := range kind.paths {
			if code, msg := checkVendorPath(p); code != "" {
				ps.err(fmt.Sprintf("/sandbox/vendorPaths/%s/%d", kind.name, i), code, "%s", msg)
			}
		}
	}
	for i, h := range m.Sandbox.VendorHosts {
		rest := strings.TrimPrefix(h, "*.")
		if rest == "" || strings.Contains(h, "://") || strings.ContainsAny(rest, "/*") {
			ps.err(fmt.Sprintf("/sandbox/vendorHosts/%d", i), "sandbox-host", "%q must be a host name, optionally starting with *.", h)
		}
	}
}
