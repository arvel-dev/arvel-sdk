// SPDX-License-Identifier: Apache-2.0

package jsoneq

import (
	"strings"
	"testing"
)

func TestEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"key order", `{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
		{"null vs absent", `{"a":1,"b":null}`, `{"a":1}`, true},
		{"empty array vs absent", `{"a":1,"b":[]}`, `{"a":1}`, true},
		{"empty object vs absent", `{"a":1,"b":{}}`, `{"a":1}`, true},
		{"nested empties", `{"a":{"b":null}}`, `{}`, true},
		{"number forms", `{"a":5.0}`, `{"a":5}`, true},
		{"different number", `{"a":1}`, `{"a":2}`, false},
		{"different string", `{"a":"x"}`, `{"a":"y"}`, false},
		{"array order matters", `[1,2]`, `[2,1]`, false},
		{"array length", `[1]`, `[1,2]`, false},
		{"missing key", `{"a":1}`, `{"a":1,"b":2}`, false},
		{"false is not empty", `{"a":false}`, `{}`, false},
		{"empty string is not empty", `{"a":""}`, `{}`, false},
	}
	for _, c := range cases {
		got, msg := Equal([]byte(c.a), []byte(c.b))
		if got != c.want {
			t.Errorf("%s: got %v (%s)", c.name, got, msg)
		}
		if !got && msg == "" {
			t.Errorf("%s: empty diff message", c.name)
		}
	}
}

func TestEqualErrors(t *testing.T) {
	if ok, msg := Equal([]byte(`{`), []byte(`{}`)); ok || !strings.HasPrefix(msg, "left:") {
		t.Errorf("left: %v %q", ok, msg)
	}
	if ok, msg := Equal([]byte(`{}`), []byte(`nope`)); ok || !strings.HasPrefix(msg, "right:") {
		t.Errorf("right: %v %q", ok, msg)
	}
	if ok, _ := Equal([]byte(`{} {}`), []byte(`{}`)); ok {
		t.Error("trailing data accepted")
	}
}

func TestDiffPath(t *testing.T) {
	_, msg := Equal([]byte(`{"a":{"b":[1,{"c":1}]}}`), []byte(`{"a":{"b":[1,{"c":2}]}}`))
	if !strings.Contains(msg, "$.a.b[1].c") {
		t.Fatalf("got %q", msg)
	}
}
