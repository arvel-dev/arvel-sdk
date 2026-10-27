// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"reflect"
	"testing"
)

func TestParseFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want flags
	}{
		{
			name: "defaults with binary",
			args: []string{"--", "bin"},
			want: flags{level: "basic", scenarios: "./conformance", manifest: "arvel.json", binary: "bin"},
		},
		{
			name: "all flags and args after --",
			args: []string{"--level", "release", "--scenarios", "conformance", "--manifest", "m.json", "--package", "pkg", "--json", "--", "bin", "--flag", "value"},
			want: flags{level: "release", scenarios: "conformance", manifest: "m.json", pkg: "pkg", json: true, binary: "bin", args: []string{"--flag", "value"}},
		},
		{
			name: "binary without -- separator",
			args: []string{"--level", "basic", "bin"},
			want: flags{level: "basic", scenarios: "./conformance", manifest: "arvel.json", binary: "bin"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			got, err := parseFlags(c.args, &buf)
			if err != nil {
				t.Fatalf("parseFlags: %v", err)
			}
			if !reflect.DeepEqual(*got, c.want) {
				t.Fatalf("got %+v, want %+v", *got, c.want)
			}
		})
	}
}

func TestParseFlags_UnknownFlag(t *testing.T) {
	var buf bytes.Buffer
	if _, err := parseFlags([]string{"--nope"}, &buf); err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
}
