// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

var (
	roleIDPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)
	skillNameRegex = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// cleanPackagePath validates a path inside the release archive: relative,
// slash separated and already clean.
func cleanPackagePath(p string) (string, bool) {
	if p == "" || path.IsAbs(p) || strings.Contains(p, `\`) {
		return "", false
	}
	c := path.Clean(p)
	if c != p || c == ".." || strings.HasPrefix(c, "../") {
		return "", false
	}
	return c, true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// localizedChecks requires the "en" text and warns about other keys (PROTOCOL
// §16.5).
func localizedChecks(ps *problems, base string, l Localized) {
	if strings.TrimSpace(l["en"]) == "" {
		ps.err(base+"/en", "locale-en", "the en text is required")
	}
	for _, k := range sortedKeys(l) {
		switch {
		case k != "en" && k != "ru":
			ps.warn(base+"/"+k, "locale-unknown", "language %q is not used: only en and ru are defined", k)
		case k == "ru" && strings.TrimSpace(l[k]) == "":
			ps.warn(base+"/ru", "locale-empty", "the ru text is empty")
		}
	}
}

// checkDeclarative applies the contributes rules of PROTOCOL §16 to a team or
// integration plugin.
func (m *Manifest) checkDeclarative(ps *problems) {
	c := m.Contributes
	if c == nil {
		return // the schema reports the missing block
	}
	if m.Type == TypeTeam {
		if len(c.MCPServers) > 0 {
			ps.err("/contributes/mcpServers", "team-mcp", "team plugins must not have mcpServers")
		}
		if len(c.Skills) > 0 {
			ps.err("/contributes/skills", "team-skills", "team plugins must not have skills")
		}
		if m.Entry.IsCommand() || len(m.Entry.Platforms) > 0 {
			ps.err("/entry", "team-entry", "team plugins have no entry")
		}
		if len(c.Roles) == 0 && len(c.TaskTypes) == 0 {
			ps.warn("/contributes", "team-empty", "a team plugin should add at least one role or task type")
		}
	}
	if m.Type == TypeIntegration && len(c.MCPServers)+len(c.Skills) == 0 {
		ps.err("/contributes", "integration-empty", "an integration plugin needs at least one MCP server or skill")
	}

	slots := map[string]bool{}
	for _, s := range protocol.Order() {
		slots[s] = true
	}
	for _, r := range c.Roles {
		if roleIDPattern.MatchString(r.ID) {
			slots[protocol.PluginSlot(m.ID, r.ID)] = true
		}
	}
	m.checkRoles(ps, c.Roles, slots)
	m.checkTaskTypes(ps, c.TaskTypes, slots)
	m.checkMCPServers(ps, c.MCPServers)
	checkSkills(ps, c.Skills)
}

func (m *Manifest) checkRoles(ps *problems, roles []RoleSpec, slots map[string]bool) {
	seen := map[string]bool{}
	after := map[string]string{} // plugin role slot -> plugin role slot it follows
	for i, r := range roles {
		base := fmt.Sprintf("/contributes/roles/%d", i)
		if !roleIDPattern.MatchString(r.ID) {
			ps.err(base+"/id", "role-id", "role id %q must match %s", r.ID, roleIDPattern)
		} else if seen[r.ID] {
			ps.err(base+"/id", "role-duplicate", "role id %q is used twice", r.ID)
		}
		seen[r.ID] = true

		switch {
		case !r.Kind.Valid():
			ps.err(base+"/kind", "role-kind", "kind %q is not valid", r.Kind)
		case r.Kind == protocol.RoleKindIntake || r.Kind == protocol.RoleKindAccept || r.Kind == protocol.RoleKindKnowledge:
			ps.err(base+"/kind", "role-kind-reserved", "kind %q is reserved for the built-in roles", r.Kind)
		}

		if (r.Placement.After != "") == r.Placement.OnCall {
			ps.err(base+"/placement", "role-placement", "set exactly one of after and onCall")
		} else if r.Placement.After != "" {
			if !slots[r.Placement.After] {
				ps.err(base+"/placement/after", "placement-slot", "%q is not a slot of the order or a role of this plugin", r.Placement.After)
			} else if roleIDPattern.MatchString(r.ID) && strings.HasPrefix(r.Placement.After, m.ID+":") {
				after[protocol.PluginSlot(m.ID, r.ID)] = r.Placement.After
			}
		}

		localizedChecks(ps, base+"/name", r.Name)
		localizedChecks(ps, base+"/description", r.Description)
		localizedChecks(ps, base+"/instructions", r.Instructions)
		for _, lang := range sortedKeys(r.Instructions) {
			if _, ok := cleanPackagePath(r.Instructions[lang]); !ok {
				ps.err(base+"/instructions/"+lang, "path-invalid", "%q must be a clean relative path inside the package", r.Instructions[lang])
			}
		}

		if r.Kind == protocol.RoleKindRead && r.Permissions != nil && r.Permissions.Edit == protocol.PermissionLevelAllow {
			ps.err(base+"/permissions/edit", "role-read-edit", "a read role never gets edit: allow")
		}
	}
	// A cycle among this plugin's own roles: report it at the first role in
	// document order that lies on one.
	for i, r := range roles {
		if !roleIDPattern.MatchString(r.ID) {
			continue
		}
		slot := protocol.PluginSlot(m.ID, r.ID)
		cur := slot
		for steps := 0; steps <= len(after); steps++ {
			next, ok := after[cur]
			if !ok {
				break
			}
			if next == slot {
				ps.err(fmt.Sprintf("/contributes/roles/%d/placement/after", i), "placement-cycle", "role %q is placed after itself through other roles", r.ID)
				return
			}
			cur = next
		}
	}
}

func (m *Manifest) checkTaskTypes(ps *problems, types []TaskTypeSpec, slots map[string]bool) {
	seen := map[string]bool{}
	for i, t := range types {
		base := fmt.Sprintf("/contributes/taskTypes/%d", i)
		if !roleIDPattern.MatchString(t.ID) {
			ps.err(base+"/id", "type-id", "task type id %q must match %s", t.ID, roleIDPattern)
		} else if seen[t.ID] {
			ps.err(base+"/id", "type-duplicate", "task type id %q is used twice", t.ID)
		}
		seen[t.ID] = true
		localizedChecks(ps, base+"/name", t.Name)
		localizedChecks(ps, base+"/description", t.Description)
		if !t.Branch.Valid() {
			ps.err(base+"/branch", "type-branch", "branch %q is not valid", t.Branch)
		}
		if !t.Result.Valid() {
			ps.err(base+"/result", "type-result", "result %q is not valid", t.Result)
		}
		for _, slot := range sortedKeys(t.Roles) {
			state := t.Roles[slot]
			p := base + "/roles/" + slot
			switch {
			case !slots[slot]:
				ps.err(p, "type-slot-unknown", "%q is not a slot of the order or a role of this plugin", slot)
			case !state.Valid():
				ps.err(p, "type-state", "state %q is not valid", state)
			case state == protocol.SlotStateAuto && slot != protocol.SlotSecurity:
				ps.err(p, "type-auto-slot", "auto is valid only for security")
			}
		}
		if t.Result == ResultCode && t.Roles[protocol.SlotLeadAccept] != protocol.SlotStateOn {
			ps.err(base+"/roles/"+protocol.SlotLeadAccept, "type-lead-accept", "lead_accept must be on for result code")
		}
	}
}

func (m *Manifest) hasEntry() bool { return m.Entry.IsCommand() || len(m.Entry.Platforms) > 0 }

func (m *Manifest) checkMCPServers(ps *problems, servers []MCPServerSpec) {
	seen := map[string]bool{}
	for i, s := range servers {
		base := fmt.Sprintf("/contributes/mcpServers/%d", i)
		if s.Name == "" || seen[s.Name] {
			ps.err(base+"/name", "mcp-name", "MCP server name %q is empty or used twice", s.Name)
		}
		seen[s.Name] = true
		if (len(s.Command) > 0) == (s.URL != "") {
			ps.err(base, "mcp-transport", "set exactly one of command and url")
		}
		if len(s.Command) > 0 {
			if s.Command[0] != "{bin}" {
				ps.err(base+"/command/0", "mcp-command", "stdio servers start the plugin's own binary: the first element must be {bin}")
			} else if !m.hasEntry() {
				ps.err(base+"/command/0", "mcp-no-entry", "{bin} needs an entry with the plugin's binary")
			}
		}
		if s.URL != "" {
			u, err := url.Parse(s.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
				ps.err(base+"/url", "mcp-url", "%q must be an http or https URL", s.URL)
			} else {
				host := normHost(u.Hostname())
				have := make([]string, 0, len(m.Permissions.Network))
				for _, n := range m.Permissions.Network {
					have = append(have, normHost(n))
				}
				if !hostCovered(have, host) {
					ps.warn(base+"/url", "mcp-url-network", "host %q is not listed in permissions.network", host)
				}
			}
		}
		tools := map[string]bool{}
		for j, tool := range s.Tools {
			if tool == "" || tools[tool] {
				ps.err(fmt.Sprintf("%s/tools/%d", base, j), "mcp-tools", "tool name %q is empty or used twice", tool)
			}
			tools[tool] = true
		}
	}
}

func checkSkills(ps *problems, skills []SkillSpec) {
	seen := map[string]bool{}
	for i, s := range skills {
		base := fmt.Sprintf("/contributes/skills/%d", i)
		if !skillNameRegex.MatchString(s.Name) || len(s.Name) > 64 {
			ps.err(base+"/name", "skill-name", "skill name %q must be lower-case words joined by hyphens, at most 64 characters", s.Name)
		} else if seen[s.Name] {
			ps.err(base+"/name", "skill-duplicate", "skill name %q is used twice", s.Name)
		}
		seen[s.Name] = true
		if _, ok := cleanPackagePath(s.Path); !ok {
			ps.err(base+"/path", "path-invalid", "%q must be a clean relative path inside the package", s.Path)
		}
	}
}
