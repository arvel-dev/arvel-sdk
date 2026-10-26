// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

func TestContributes_Rules(t *testing.T) {
	const team, integ = "valid-team.json", "valid-integration.json"
	role := func(m *Manifest) *RoleSpec { return &m.Contributes.Roles[0] }
	typ := func(m *Manifest) *TaskTypeSpec { return &m.Contributes.TaskTypes[0] }
	cases := []mutation{
		{name: "team mcp", fixture: team, mutate: func(m *Manifest) {
			m.Contributes.MCPServers = []MCPServerSpec{{Name: "x", URL: "https://a.example.com"}}
		}, code: "team-mcp"},
		{name: "team skills", fixture: team, mutate: func(m *Manifest) { m.Contributes.Skills = []SkillSpec{{Name: "x", Path: "skills/x"}} }, code: "team-skills"},
		{name: "team entry", fixture: team, mutate: func(m *Manifest) { m.Entry = EntrySpec{Command: []string{"x"}} }, code: "team-entry"},
		{name: "team empty", fixture: team, mutate: func(m *Manifest) { m.Contributes = &Contributes{} }, code: "team-empty", severity: SeverityWarning},
		{name: "integration empty", fixture: integ, mutate: func(m *Manifest) { m.Contributes = &Contributes{} }, code: "integration-empty"},
		{name: "role id", fixture: team, mutate: func(m *Manifest) { role(m).ID = "A" }, code: "role-id"},
		{name: "role duplicate", fixture: team, mutate: func(m *Manifest) {
			r := *role(m)
			m.Contributes.Roles = append(m.Contributes.Roles, r)
		}, code: "role-duplicate", path: "/contributes/roles/1/id"},
		{name: "role kind unknown", fixture: team, mutate: func(m *Manifest) { role(m).Kind = "wizard" }, code: "role-kind"},
		{name: "role kind intake", fixture: team, mutate: func(m *Manifest) { role(m).Kind = protocol.RoleKindIntake }, code: "role-kind-reserved"},
		{name: "role kind knowledge", fixture: team, mutate: func(m *Manifest) { role(m).Kind = protocol.RoleKindKnowledge }, code: "role-kind-reserved"},
		{name: "placement none", fixture: team, mutate: func(m *Manifest) { role(m).Placement = Placement{} }, code: "role-placement"},
		{name: "placement both", fixture: team, mutate: func(m *Manifest) { role(m).Placement = Placement{After: "tester", OnCall: true} }, code: "role-placement"},
		{name: "placement unknown slot", fixture: team, mutate: func(m *Manifest) { role(m).Placement.After = "keeper" }, code: "placement-slot"},
		{name: "placement self", fixture: team, mutate: func(m *Manifest) { role(m).Placement.After = "team-demo:docs-writer" }, code: "placement-cycle"},
		{name: "placement on call ok", fixture: team, mutate: func(m *Manifest) { role(m).Placement = Placement{OnCall: true} }, code: ""},
		{name: "role read edit", fixture: team, mutate: func(m *Manifest) {
			role(m).Kind = protocol.RoleKindRead
			role(m).Permissions.Edit = protocol.PermissionLevelAllow
		}, code: "role-read-edit"},
		{name: "locale en name", fixture: team, mutate: func(m *Manifest) { role(m).Name = Localized{"ru": "x"} }, code: "locale-en", path: "/contributes/roles/0/name/en"},
		{name: "locale unknown", fixture: team, mutate: func(m *Manifest) { role(m).Name["de"] = "x" }, code: "locale-unknown", severity: SeverityWarning},
		{name: "locale empty ru", fixture: team, mutate: func(m *Manifest) { role(m).Name["ru"] = " " }, code: "locale-empty", severity: SeverityWarning},
		{name: "instructions absolute", fixture: team, mutate: func(m *Manifest) { role(m).Instructions["en"] = "/etc/passwd" }, code: "path-invalid"},
		{name: "instructions dotdot", fixture: team, mutate: func(m *Manifest) { role(m).Instructions["en"] = "../x.md" }, code: "path-invalid"},
		{name: "instructions unclean", fixture: team, mutate: func(m *Manifest) { role(m).Instructions["en"] = "roles/./x.md" }, code: "path-invalid"},
		{name: "instructions backslash", fixture: team, mutate: func(m *Manifest) { role(m).Instructions["en"] = `roles\x.md` }, code: "path-invalid"},
		{name: "type id", fixture: team, mutate: func(m *Manifest) { typ(m).ID = "Docs" }, code: "type-id"},
		{name: "type duplicate", fixture: team, mutate: func(m *Manifest) {
			m.Contributes.TaskTypes = append(m.Contributes.TaskTypes, *typ(m))
		}, code: "type-duplicate"},
		{name: "type branch", fixture: team, mutate: func(m *Manifest) { typ(m).Branch = "fork" }, code: "type-branch"},
		{name: "type result", fixture: team, mutate: func(m *Manifest) { typ(m).Result = "essay" }, code: "type-result"},
		{name: "type unknown slot", fixture: team, mutate: func(m *Manifest) { typ(m).Roles["keeper"] = protocol.SlotStateOn }, code: "type-slot-unknown"},
		{name: "type foreign plugin slot", fixture: team, mutate: func(m *Manifest) { typ(m).Roles["other:role"] = protocol.SlotStateOn }, code: "type-slot-unknown"},
		{name: "type bad state", fixture: team, mutate: func(m *Manifest) { typ(m).Roles["reviewer"] = "maybe" }, code: "type-state"},
		{name: "type auto not security", fixture: team, mutate: func(m *Manifest) { typ(m).Roles["reviewer"] = protocol.SlotStateAuto }, code: "type-auto-slot"},
		{name: "type auto security ok", fixture: team, mutate: func(m *Manifest) { typ(m).Roles["security"] = protocol.SlotStateAuto }, code: ""},
		{name: "code needs lead_accept", fixture: team, mutate: func(m *Manifest) { delete(typ(m).Roles, "lead_accept") }, code: "type-lead-accept"},
		{name: "code lead_accept off", fixture: team, mutate: func(m *Manifest) { typ(m).Roles["lead_accept"] = protocol.SlotStateOff }, code: "type-lead-accept"},
		{name: "report without lead_accept ok", fixture: team, mutate: func(m *Manifest) {
			typ(m).Result = ResultReport
			delete(typ(m).Roles, "lead_accept")
		}, code: ""},
		{name: "mcp name", fixture: integ, mutate: func(m *Manifest) { m.Contributes.MCPServers[0].Name = "" }, code: "mcp-name"},
		{name: "mcp both transports", fixture: integ, mutate: func(m *Manifest) { m.Contributes.MCPServers[0].URL = "https://a.example.com" }, code: "mcp-transport"},
		{name: "mcp no transport", fixture: integ, mutate: func(m *Manifest) { m.Contributes.MCPServers[0].Command = nil }, code: "mcp-transport"},
		{name: "mcp command not bin", fixture: integ, mutate: func(m *Manifest) { m.Contributes.MCPServers[0].Command = []string{"godot"} }, code: "mcp-command"},
		{name: "mcp bin without entry", fixture: integ, mutate: func(m *Manifest) { m.Entry = EntrySpec{} }, code: "mcp-no-entry"},
		{name: "mcp url bad", fixture: integ, mutate: func(m *Manifest) {
			m.Contributes.MCPServers[0].Command = nil
			m.Contributes.MCPServers[0].URL = "ftp://x"
		}, code: "mcp-url"},
		{name: "mcp url network", fixture: integ, mutate: func(m *Manifest) {
			m.Contributes.MCPServers[0].Command = nil
			m.Contributes.MCPServers[0].URL = "https://mcp.example.com/v1"
		}, code: "mcp-url-network", severity: SeverityWarning},
		{name: "mcp url covered", fixture: integ, mutate: func(m *Manifest) {
			m.Contributes.MCPServers[0].Command = nil
			m.Contributes.MCPServers[0].URL = "https://mcp.example.com/v1"
			m.Permissions.Network = []string{"*.example.com"}
		}, code: ""},
		{name: "mcp tools duplicate", fixture: integ, mutate: func(m *Manifest) { m.Contributes.MCPServers[0].Tools = []string{"a", "a"} }, code: "mcp-tools"},
		{name: "skill name", fixture: integ, mutate: func(m *Manifest) { m.Contributes.Skills[0].Name = "Godot_Scenes" }, code: "skill-name"},
		{name: "skill duplicate", fixture: integ, mutate: func(m *Manifest) {
			m.Contributes.Skills = append(m.Contributes.Skills, m.Contributes.Skills[0])
		}, code: "skill-duplicate"},
		{name: "skill path", fixture: integ, mutate: func(m *Manifest) { m.Contributes.Skills[0].Path = "../skills" }, code: "path-invalid"},
		{name: "declarative sandbox", fixture: team, mutate: func(m *Manifest) { m.Sandbox = &ManifestSandbox{VendorHosts: []string{"a.example.com"}} }, code: "declarative-sandbox"},
		{name: "provider contributes", fixture: "valid-claude.json", mutate: func(m *Manifest) { m.Contributes = &Contributes{} }, code: "contributes-ignored", severity: SeverityWarning},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := fixture(t, c.fixture, true)
			c.mutate(m)
			ps := m.ValidateWith(Options{Official: true})
			if c.code == "" {
				if HasErrors(ps) {
					t.Fatalf("want no errors, got %v", ps)
				}
				return
			}
			p, ok := find(ps, c.code, c.path)
			if !ok {
				t.Fatalf("no %s problem at %q in %v", c.code, c.path, ps)
			}
			want := c.severity
			if want == "" {
				want = SeverityError
			}
			if p.Severity != want {
				t.Fatalf("severity %s, want %s (%v)", p.Severity, want, p)
			}
		})
	}
}

func TestContributes_PlacementCycle(t *testing.T) {
	build := func(afterA, afterB string) *Manifest {
		m := fixture(t, "valid-team.json", true)
		a := m.Contributes.Roles[0]
		a.Placement = Placement{After: afterA}
		b := a
		b.ID = "second-role"
		b.Placement = Placement{After: afterB}
		m.Contributes.Roles = []RoleSpec{a, b}
		m.Contributes.TaskTypes[0].Roles = map[string]protocol.SlotState{"lead_accept": protocol.SlotStateOn}
		return m
	}
	const a, b = "team-demo:docs-writer", "team-demo:second-role"
	for name, tc := range map[string]struct {
		afterA, afterB string
		cycle          bool
	}{
		"two roles": {b, a, true},
		"chain":     {b, "developer", false},
		"order":     {"tester", "tester", false},
	} {
		t.Run(name, func(t *testing.T) {
			ps := build(tc.afterA, tc.afterB).Validate()
			_, ok := find(ps, "placement-cycle", "")
			if ok != tc.cycle {
				t.Fatalf("cycle = %v, want %v (%v)", ok, tc.cycle, ps)
			}
		})
	}
}

func TestToolTypeRejected(t *testing.T) {
	m, err := Load(strings.NewReader(`{"id":"x-tool","name":"X","type":"tool","version":"0.1.0","pluginApi":"1"}`))
	if err != nil {
		t.Fatal(err)
	}
	ps := m.Validate()
	if _, ok := find(ps, "schema", "/type"); !ok {
		t.Errorf("no schema problem for the type in %v", ps)
	}
	p, ok := find(ps, "type-tool", "/type")
	if !ok || !strings.Contains(p.Message, "integration") {
		t.Errorf("no hint to use integration: %v", ps)
	}
}

func TestSchemaPresenceRules(t *testing.T) {
	const base = `"id":"demo","name":"Demo","version":"0.1.0","pluginApi":"1"`
	role := `{"id":"writer","name":{"en":"W"},"description":{"en":"W"},"kind":"code","placement":{"onCall":true},"instructions":{"en":"roles/w.md"}}`
	contrib := `"contributes":{"roles":[` + role + `]}`
	entry := `"entry":{"linux-amd64":{"asset":"a.tgz","bin":"b"}}`
	conn := `"connections":[{"kind":"local","label":"L","billing":"unknown"}]`
	cases := []struct {
		name, doc string
		valid     bool
	}{
		{"team ok", `{` + base + `,"type":"team",` + contrib + `}`, true},
		{"team entry", `{` + base + `,"type":"team",` + contrib + `,` + entry + `}`, false},
		{"team empty connections", `{` + base + `,"type":"team",` + contrib + `,"connections":[]}`, false},
		{"team no contributes", `{` + base + `,"type":"team"}`, false},
		{"integration no contributes", `{` + base + `,"type":"integration"}`, false},
		{"provider no entry", `{` + base + `,"type":"provider",` + conn + `}`, false},
		{"router no connections", `{` + base + `,"type":"router",` + entry + `}`, false},
		{"role kind tool", `{` + base + `,"type":"team","contributes":{"roles":[` + strings.Replace(role, `"code"`, `"tool"`, 1) + `]}}`, false},
		{"role without instructions", `{` + base + `,"type":"team","contributes":{"roles":[` + strings.Replace(role, `,"instructions":{"en":"roles/w.md"}`, ``, 1) + `]}}`, false},
		{"type bad state", `{` + base + `,"type":"team","contributes":{"taskTypes":[{"id":"t1","name":{"en":"T"},"description":{"en":"T"},"roles":{"developer":"maybe"},"branch":"new","result":"report"}]}}`, false},
		{"type without branch", `{` + base + `,"type":"team","contributes":{"taskTypes":[{"id":"t1","name":{"en":"T"},"description":{"en":"T"},"roles":{},"result":"report"}]}}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Load(strings.NewReader(c.doc))
			if err != nil {
				t.Fatal(err)
			}
			_, hasSchema := find(m.schemaProblems(), "schema", "")
			if hasSchema == c.valid {
				t.Fatalf("schema valid = %v, want %v (%v)", !hasSchema, c.valid, m.schemaProblems())
			}
		})
	}
}

func TestTeamManifestMarshalOmitsEntryAndConnections(t *testing.T) {
	m := fixture(t, "valid-team.json", true)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["entry"]; ok {
		t.Error("entry present")
	}
	if _, ok := got["connections"]; ok {
		t.Error("connections present")
	}
}

func TestEnumsContributes(t *testing.T) {
	for _, v := range AllBranchModes() {
		if !v.Valid() {
			t.Errorf("%q invalid", v)
		}
	}
	for _, v := range AllResultModes() {
		if !v.Valid() {
			t.Errorf("%q invalid", v)
		}
	}
	if BranchMode("").Valid() || ResultMode("x").Valid() {
		t.Error("bad values accepted")
	}
	if len(AllBranchModes()) != 4 || len(AllResultModes()) != 3 {
		t.Error("enum sizes")
	}
}

func TestSection161(t *testing.T) {
	m := fixture(t, "section-16.1.json", false)
	ps := m.Validate()
	if HasErrors(ps) {
		t.Fatalf("the document example must validate: %v", ps)
	}
	for _, p := range ps {
		if p.Code != "platform-missing" {
			t.Errorf("unexpected problem %v", p)
		}
	}
	if len(ps) != 5 {
		t.Errorf("want the 5 missing platforms, got %d problems", len(ps))
	}
	if m.Type != TypeIntegration || len(m.Contributes.Roles) != 1 || m.Contributes.TaskTypes[0].Roles["godot:scene-designer"] != protocol.SlotStateOn {
		t.Errorf("decoded manifest differs: %+v", m.Contributes)
	}
}
