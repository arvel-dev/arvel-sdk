// SPDX-License-Identifier: Apache-2.0

package fakevendor

import (
	"strings"
	"testing"
)

func loadErr(t *testing.T, yaml string) error {
	t.Helper()
	_, err := Load(strings.NewReader(yaml))
	if err == nil {
		t.Fatalf("expected an error for:\n%s", yaml)
	}
	return err
}

func TestLoad_Script(t *testing.T) {
	s, err := LoadFile("testdata/claude-basic-turn.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "claude-basic-turn" {
		t.Fatalf("name = %q", s.Name)
	}
	if s.Version != "2.1.310" {
		t.Fatalf("version = %q", s.Version)
	}
	if len(s.Steps) != 4 {
		t.Fatalf("steps = %d", len(s.Steps))
	}
}

func TestLoad_UnknownField(t *testing.T) {
	loadErr(t, "name: x\nsteps: []\nunknown: 1\n")
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"no step kind", "name: x\nsteps: [{}]\n"},
		{"two kinds", "name: x\nsteps: [{emit: {line: A}, exitCode: 0}]\n"},
		{"bad stream", "name: x\nsteps: [{emit: {stream: oops, line: A}}]\n"},
		{"json and regex", "name: x\nsteps: [{expect: {json: {a: b}, regex: x}}]\n"},
		{"bad regex", "name: x\nsteps: [{expect: {regex: '['}}]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loadErr(t, c.yaml)
		})
	}
}
