// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"reflect"
	"strings"
	"testing"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// fixture loads a testdata manifest. With typed set, the raw document is
// dropped so Validate checks what the test changed in the struct.
func fixture(t *testing.T, name string, typed bool) *Manifest {
	t.Helper()
	m, err := LoadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if typed {
		m.raw = nil
	}
	return m
}

func find(ps []Problem, code, path string) (Problem, bool) {
	for _, p := range ps {
		if p.Code == code && (path == "" || p.Path == path) {
			return p, true
		}
	}
	return Problem{}, false
}

func TestValidFixtures(t *testing.T) {
	for _, name := range []string{"valid-claude.json", "valid-router.json", "valid-command.json", "valid-team.json", "valid-integration.json"} {
		for _, typed := range []bool{false, true} {
			m := fixture(t, name, typed)
			if ps := m.ValidateWith(Options{Official: true}); len(ps) != 0 {
				t.Errorf("%s typed=%v: %v", name, typed, ps)
			}
		}
	}
}

type mutation struct {
	name     string
	fixture  string
	mutate   func(*Manifest)
	code     string
	path     string
	severity Severity
	opts     Options
}

func TestSemanticRules(t *testing.T) {
	const claude, router = "valid-claude.json", "valid-router.json"
	cases := []mutation{
		{name: "bad id", fixture: claude, mutate: func(m *Manifest) { m.ID = "A" }, code: "schema", path: "/id"},
		{name: "plugin api", fixture: claude, mutate: func(m *Manifest) { m.PluginAPI = "2" }, code: "schema", path: "/pluginApi"},
		{name: "bad type", fixture: claude, mutate: func(m *Manifest) { m.Type = "tool" }, code: "schema", path: "/type"},
		{name: "version shorthand", fixture: claude, mutate: func(m *Manifest) { m.Version = "1.0" }, code: "schema", path: "/version"},
		{name: "min version", fixture: claude, mutate: func(m *Manifest) { m.Requires[0].MinVersion = "two" }, code: "semver", path: "/requires/0/minVersion"},
		{name: "missing platform official", fixture: claude, opts: Options{Official: true},
			mutate: func(m *Manifest) { delete(m.Entry.Platforms, "windows-arm64") },
			code:   "platform-missing", path: "/entry/windows-arm64", severity: SeverityError},
		{name: "missing platform third party", fixture: claude,
			mutate: func(m *Manifest) { delete(m.Entry.Platforms, "windows-arm64") },
			code:   "platform-missing", path: "/entry/windows-arm64", severity: SeverityWarning},
		{name: "unknown platform", fixture: claude,
			mutate: func(m *Manifest) { m.Entry.Platforms["freebsd-amd64"] = Entry{Asset: "a", Bin: "b"} },
			code:   "platform-unknown", severity: SeverityWarning},
		{name: "no checksums", fixture: claude, mutate: func(m *Manifest) { m.Checksums = "" }, code: "checksums-missing"},
		{name: "spawn missing", fixture: claude, mutate: func(m *Manifest) { m.Permissions.Spawn = nil }, code: "spawn-missing", path: "/requires/0/binary"},
		{name: "command not in spawn", fixture: "valid-command.json", mutate: func(m *Manifest) { m.Permissions.Spawn = nil }, code: "command-not-in-spawn"},
		{name: "command not in requires", fixture: "valid-command.json", mutate: func(m *Manifest) { m.Requires = nil }, code: "command-not-in-requires"},
		{name: "duplicate kind", fixture: claude, mutate: func(m *Manifest) { m.Connections[1].Kind = m.Connections[0].Kind }, code: "kind-duplicate", path: "/connections/1/kind"},
		{name: "bad billing", fixture: claude, mutate: func(m *Manifest) { m.Connections[0].Billing = "free" }, code: "billing", path: "/connections/0/billing"},
		{name: "duplicate secret", fixture: claude,
			mutate: func(m *Manifest) {
				m.Connections[1].Secrets = append(m.Connections[1].Secrets, m.Connections[1].Secrets[0])
			},
			code: "secret-duplicate", path: "/connections/1/secrets/1/id"},
		{name: "secret pattern", fixture: claude, mutate: func(m *Manifest) { m.Connections[1].Secrets[0].Pattern = "(" }, code: "secret-pattern"},
		{name: "config type", fixture: claude, mutate: func(m *Manifest) { m.Connections[0].Config.Type = "array" }, code: "config-subset", path: "/connections/0/config/type"},
		{name: "config nested", fixture: claude,
			mutate: func(m *Manifest) { m.Connections[0].Config.Properties["n"] = ConfigProperty{Type: "object"} },
			code:   "config-subset", path: "/connections/0/config/properties/n"},
		{name: "enum on integer", fixture: claude,
			mutate: func(m *Manifest) {
				m.Connections[0].Config.Properties["n"] = ConfigProperty{Type: "integer", Enum: []string{"a"}}
			},
			code: "config-subset"},
		{name: "default outside bounds", fixture: claude,
			mutate: func(m *Manifest) {
				lo, hi := int64(1), int64(5)
				m.Connections[0].Config.Properties["n"] = ConfigProperty{Type: "integer", Minimum: &lo, Maximum: &hi, Default: float64(9)}
			},
			code: "config-subset"},
		{name: "min above max", fixture: claude,
			mutate: func(m *Manifest) {
				lo, hi := int64(5), int64(1)
				m.Connections[0].Config.Properties["n"] = ConfigProperty{Type: "integer", Minimum: &lo, Maximum: &hi}
			},
			code: "config-subset"},
		{name: "fractional default", fixture: claude,
			mutate: func(m *Manifest) {
				m.Connections[0].Config.Properties["n"] = ConfigProperty{Type: "integer", Default: 1.5}
			},
			code: "config-subset"},
		{name: "string default not in enum", fixture: claude,
			mutate: func(m *Manifest) {
				m.Connections[0].Config.Properties["s"] = ConfigProperty{Type: "string", Enum: []string{"a", "b"}, Default: "c"}
			},
			code: "config-subset"},
		{name: "repeated enum", fixture: claude,
			mutate: func(m *Manifest) {
				m.Connections[0].Config.Properties["s"] = ConfigProperty{Type: "string", Enum: []string{"a", "a"}}
			},
			code: "config-subset"},
		{name: "bad string pattern", fixture: claude,
			mutate: func(m *Manifest) {
				m.Connections[0].Config.Properties["s"] = ConfigProperty{Type: "string", Pattern: "("}
			},
			code: "config-subset"},
		{name: "boolean default", fixture: claude,
			mutate: func(m *Manifest) {
				m.Connections[0].Config.Properties["b"] = ConfigProperty{Type: "boolean", Default: "yes"}
			},
			code: "config-subset"},
		{name: "unknown files", fixture: claude, mutate: func(m *Manifest) { m.Permissions.Files = "everywhere" }, code: "files-unknown", severity: SeverityWarning},
		{name: "router no features", fixture: router, mutate: func(m *Manifest) { m.Capabilities.Router.Features = nil }, code: "router-features"},
		{name: "router bad feature", fixture: router, mutate: func(m *Manifest) { m.Capabilities.Router.Features = []string{"route.fly"} }, code: "router-features"},
		{name: "router no connection", fixture: router, mutate: func(m *Manifest) { m.Connections = nil }, code: "router-connection"},
		{name: "router secrets without network", fixture: router,
			mutate: func(m *Manifest) { m.Connections[0].Secrets = []Secret{{ID: "key", Label: "Key"}} },
			code:   "router-network"},
		{name: "router sandbox", fixture: router, mutate: func(m *Manifest) { m.Sandbox = &ManifestSandbox{VendorHosts: []string{"a.example.com"}} }, code: "router-sandbox"},
		{name: "router spawn", fixture: router, mutate: func(m *Manifest) { m.Permissions.Spawn = []string{"x"} }, code: "router-spawn", severity: SeverityWarning},
		{name: "provider router caps", fixture: claude, mutate: func(m *Manifest) { m.Capabilities.Router = &RouterCapabilities{} }, code: "provider-router-caps", severity: SeverityWarning},
		{name: "forbidden vendor path", fixture: claude,
			mutate: func(m *Manifest) {
				m.Sandbox.VendorPaths.Write = append(m.Sandbox.VendorPaths.Write, "~/.aws/credentials")
			},
			code: "sandbox-path-forbidden", path: "/sandbox/vendorPaths/write/2"},
		{name: "vendor host scheme", fixture: claude,
			mutate: func(m *Manifest) { m.Sandbox.VendorHosts = append(m.Sandbox.VendorHosts, "https://x.com") },
			code:   "sandbox-host"},
		{name: "vendor host wildcard", fixture: claude,
			mutate: func(m *Manifest) { m.Sandbox.VendorHosts = append(m.Sandbox.VendorHosts, "a.*.com") },
			code:   "sandbox-host"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := fixture(t, c.fixture, true)
			c.mutate(m)
			ps := m.ValidateWith(c.opts)
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

func TestInvalidFixtures(t *testing.T) {
	cases := []struct{ file, code, path string }{
		{"invalid-id.json", "schema", "/id"},
		{"invalid-pluginapi.json", "schema", "/pluginApi"},
		{"invalid-forbidden-path.json", "sandbox-path-forbidden", "/sandbox/vendorPaths/read/2"},
		{"invalid-router-sandbox.json", "router-sandbox", "/sandbox"},
		{"invalid-spawn.json", "spawn-missing", "/requires/0/binary"},
		{"invalid-config-nested.json", "schema", ""},
		{"invalid-tool-type.json", "type-tool", "/type"},
		{"invalid-team-with-entry.json", "schema", ""},
		{"invalid-role-kind.json", "role-kind-reserved", "/contributes/roles/0/kind"},
		{"invalid-type-slot.json", "type-slot-unknown", "/contributes/taskTypes/0/roles/other:role"},
		{"invalid-placement-cycle.json", "placement-cycle", "/contributes/roles/0/placement/after"},
		{"invalid-instructions-en.json", "locale-en", "/contributes/roles/0/instructions/en"},
	}
	for _, c := range cases {
		ps := fixture(t, c.file, false).ValidateWith(Options{Official: true})
		p, ok := find(ps, c.code, c.path)
		if !ok || p.Severity != SeverityError {
			t.Errorf("%s: no error %s at %q in %v", c.file, c.code, c.path, ps)
		}
		if !HasErrors(ps) {
			t.Errorf("%s: HasErrors false", c.file)
		}
	}
}

func TestSchemaOnRawDocument(t *testing.T) {
	m, err := Load(strings.NewReader(`{"id":"x","entry":{"linux-amd64":{"asset":"a"}},"connections":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	ps := m.Validate()
	for _, want := range []string{"/name", "/entry/linux-amd64", "/connections"} {
		found := false
		for _, p := range ps {
			if p.Code == "schema" && strings.HasPrefix(p.Path, want) || p.Code == "schema" && p.Path == "" {
				found = true
			}
		}
		if !found {
			t.Errorf("no schema problem for %s in %v", want, ps)
		}
	}
}

func TestSchemaAcceptsUnknownFields(t *testing.T) {
	doc, err := LoadFile("testdata/valid-claude.json")
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(string(doc.raw), `"id": "claude",`, `"id": "claude", "x-future": {"a": 1},`, 1)
	raw = strings.Replace(raw, `"kind": "subscription",`, `"kind": "subscription", "x-note": true,`, 1)
	m, err := Load(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if ps := m.ValidateWith(Options{Official: true}); len(ps) != 0 {
		t.Fatalf("unknown fields must be ignored: %v", ps)
	}
}

func TestValidateZeroManifest(t *testing.T) {
	ps := (&Manifest{}).Validate()
	if !HasErrors(ps) {
		t.Fatal("zero manifest must not be valid")
	}
	if _, ok := find(ps, "schema", ""); !ok {
		t.Fatalf("no schema problem in %v", ps)
	}
}

func TestProblemOrderDeterministic(t *testing.T) {
	build := func() []Problem {
		m := fixture(t, "valid-claude.json", true)
		m.ID = "A"
		m.Version = "1.0"
		delete(m.Entry.Platforms, "linux-arm64")
		m.Connections[1].Kind = m.Connections[0].Kind
		return m.Validate()
	}
	if a, b := build(), build(); !reflect.DeepEqual(a, b) {
		t.Fatalf("order differs:\n%v\n%v", a, b)
	}
}

func TestProblemString(t *testing.T) {
	p := Problem{Severity: SeverityWarning, Path: "/x", Code: "c", Message: "m"}
	if got := p.String(); got != "warning /x [c]: m" {
		t.Fatalf("got %q", got)
	}
	if HasErrors([]Problem{p}) {
		t.Fatal("warning is not an error")
	}
}

func TestBillingFromProtocol(t *testing.T) {
	m := fixture(t, "valid-claude.json", false)
	if m.Connections[0].Billing != protocol.BillingSubscription {
		t.Fatalf("billing = %q", m.Connections[0].Billing)
	}
}
