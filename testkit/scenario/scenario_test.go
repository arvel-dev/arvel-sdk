// SPDX-License-Identifier: Apache-2.0

package scenario

import (
	"reflect"
	"strings"
	"testing"
)

func loadErr(t *testing.T, s string) error {
	t.Helper()
	_, err := Load(strings.NewReader(s))
	if err == nil {
		t.Fatalf("expected an error for:\n%s", s)
	}
	return err
}

func TestLoad_SpecExample(t *testing.T) {
	s, err := LoadFile("testdata/basic-turn.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "basic-turn" {
		t.Fatalf("name = %q", s.Name)
	}
	if s.Plugin.Env["ARVEL_CLAUDE_BIN"] != "{{fakevendor:claude-basic-turn}}" {
		t.Fatalf("env = %v", s.Plugin.Env)
	}
	if s.Initialize.Connection.Kind != "subscription" {
		t.Fatalf("connection = %+v", s.Initialize.Connection)
	}
	if len(s.Steps) != 3 {
		t.Fatalf("steps = %d", len(s.Steps))
	}
	if s.Steps[1].Save["session"] != "$.sessionId" {
		t.Fatalf("save = %v", s.Steps[1].Save)
	}
	if s.Steps[2].AnswerPermissions != "allow_once" {
		t.Fatalf("answerPermissions = %q", s.Steps[2].AnswerPermissions)
	}
	if len(s.Steps[2].ExpectUpdates) != 1 || s.Steps[2].ExpectUpdates[0].SessionUpdate != "agent_message_chunk" {
		t.Fatalf("expectUpdates = %v", s.Steps[2].ExpectUpdates)
	}
}

func TestLoad_ExpectError(t *testing.T) {
	s, err := LoadFile("testdata/expect-error.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if s.Steps[0].ExpectError == nil || s.Steps[0].ExpectError.Code != -32601 {
		t.Fatalf("expectError = %+v", s.Steps[0].ExpectError)
	}
}

func TestLoad_UnknownField(t *testing.T) {
	loadErr(t, "name: x\nsteps: []\nunknown: 1\n")
}

func TestLoad_MissingSteps(t *testing.T) {
	loadErr(t, "name: x\n")
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"empty name", "name: \"\"\nsteps: [{call: _arvel/status}]\n"},
		{"empty call", "name: x\nsteps: [{call: \"\"}]\n"},
		{"bad answerPermissions", "name: x\nsteps: [{call: _arvel/status, answerPermissions: nope}]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loadErr(t, c.yaml)
		})
	}
}

func TestResolveString(t *testing.T) {
	r := Resolver{Vars: map[string]string{"session": "s_1"}, Tmpdir: "/tmp/t1"}
	if got, err := r.ResolveString("cwd={{tmpdir}}"); err != nil || got != "cwd=/tmp/t1" {
		t.Fatalf("tmpdir: %q %v", got, err)
	}
	if got, err := r.ResolveString("{{session}}"); err != nil || got != "s_1" {
		t.Fatalf("var: %q %v", got, err)
	}
	if _, err := r.ResolveString("{{missing}}"); err == nil {
		t.Fatal("missing var did not error")
	}
	if _, err := r.ResolveString("{{fakevendor:x}}"); err == nil {
		t.Fatal("fakevendor without hook did not error")
	}
	withFv := Resolver{Vars: map[string]string{}, Fakevendor: func(name string) (string, error) { return "/bin/" + name, nil }}
	if got, err := withFv.ResolveString("{{fakevendor:x}}"); err != nil || got != "/bin/x" {
		t.Fatalf("fakevendor: %q %v", got, err)
	}
	if _, err := r.ResolveString("{{unterminated"); err == nil {
		t.Fatal("unterminated did not error")
	}
	if _, err := r.ResolveString("prefix}}-{{tmpdir}}"); err == nil {
		t.Fatal("stray closing delimiter accepted")
	}
}

func TestLoad_InitializeAliasesAndMerge(t *testing.T) {
	for _, source := range []string{
		"name: aliases\ninitialize:\n  <<: &base\n    connection: {kind: synthetic}\n  dataDir: '{{tmpdir}}'\nsteps: [{call: _arvel/status}]\n",
		"name: aliases\ninitialize:\n  connection:\n    <<: &connection {kind: synthetic, config: {enabled: false}}\n    secrets: {token: synthetic}\nsteps: [{call: _arvel/status}]\n",
	} {
		sc, err := Load(strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		if !sc.Initialize.KindSpecified() || sc.Initialize.Connection.Kind != "synthetic" {
			t.Fatal("alias kind lost")
		}
	}
}

func TestLoad_ShallowMergeKindPresence(t *testing.T) {
	for _, override := range []string{"{config: {count: 2}}", "null"} {
		input := "name: shadow\ninitialize:\n  <<: {connection: {kind: synthetic}}\n  connection: " + override + "\nsteps: [{call: _arvel/status}]"
		sc, err := Load(strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		if sc.Initialize.KindSpecified() {
			t.Fatal("shadowed YAML kind was treated as explicit")
		}
	}
}

func TestLoad_InvalidPermissionDiagnosticOmitsValue(t *testing.T) {
	_, err := Load(strings.NewReader("name: invalid\nsteps: [{call: _arvel/status, answerPermissions: private-sentinel}]"))
	if err == nil || strings.Contains(err.Error(), "private-sentinel") {
		t.Fatal("invalid permission diagnostic exposed input")
	}
}

func TestResolve_Recursive(t *testing.T) {
	r := Resolver{Vars: map[string]string{"m": "sonnet"}, Tmpdir: "/t"}
	in := map[string]any{"_meta": map[string]any{"arvel": map[string]any{"model": "{{m}}"}}, "cwd": "{{tmpdir}}", "list": []any{"{{m}}"}}
	out, err := r.Resolve(in)
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["cwd"] != "/t" {
		t.Fatalf("cwd = %v", m["cwd"])
	}
	if m["_meta"].(map[string]any)["arvel"].(map[string]any)["model"] != "sonnet" {
		t.Fatalf("meta = %v", m["_meta"])
	}
	if m["list"].([]any)[0] != "sonnet" {
		t.Fatalf("list = %v", m["list"])
	}
}

func TestSubset(t *testing.T) {
	got := map[string]any{"state": "ready", "message": "hi", "count": float64(3), "nested": map[string]any{"a": float64(1)}, "list": []any{"x", "y"}}
	cases := []struct {
		name string
		want map[string]any
		ok   bool
	}{
		{"equal", map[string]any{"state": "ready"}, true},
		{"nested", map[string]any{"nested": map[string]any{"a": float64(1)}}, true},
		{"int vs float", map[string]any{"count": 3}, true},
		{"missing key", map[string]any{"nope": 1}, false},
		{"type mismatch", map[string]any{"state": 1}, false},
		{"slice", map[string]any{"list": []any{"x", "y"}}, true},
		{"slice len", map[string]any{"list": []any{"x"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Subset(c.want, got); got != c.ok {
				t.Fatalf("Subset(%v, %v) = %v, want %v", c.want, got, got, c.ok)
			}
		})
	}
}

func TestJSONPath(t *testing.T) {
	root := map[string]any{"sessionId": "s_1", "a": map[string]any{"b": "x"}}
	cases := []struct {
		path string
		want any
		ok   bool
	}{
		{"$", root, true},
		{"$.sessionId", "s_1", true},
		{"$.a.b", "x", true},
		{"$.missing", nil, false},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			got, ok := JSONPath(root, c.path)
			if ok != c.ok {
				t.Fatalf("JSONPath(%q) ok = %v, want %v", c.path, ok, c.ok)
			}
			if ok && !reflect.DeepEqual(got, c.want) {
				t.Fatalf("JSONPath(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestMatchUpdates(t *testing.T) {
	updates := []map[string]any{
		{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"text": "hello"}},
		{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"text": "world"}},
	}
	cases := []struct {
		name    string
		expects []UpdateExpect
		ok      bool
	}{
		{"satisfied", []UpdateExpect{{SessionUpdate: "agent_message_chunk"}}, true},
		{"subset", []UpdateExpect{{SessionUpdate: "agent_message_chunk", Subset: map[string]any{"content": map[string]any{"text": "world"}}}}, true},
		{"wrong kind", []UpdateExpect{{SessionUpdate: "tool_call"}}, false},
		{"subset mismatch", []UpdateExpect{{SessionUpdate: "agent_message_chunk", Subset: map[string]any{"content": map[string]any{"text": "nope"}}}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := MatchUpdates(updates, c.expects)
			if (err == nil) != c.ok {
				t.Fatalf("MatchUpdates = %v, want ok=%v", err, c.ok)
			}
		})
	}
}
