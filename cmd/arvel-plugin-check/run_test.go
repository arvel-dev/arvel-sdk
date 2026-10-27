// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"testing"
)

func TestRun_UsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no args", nil},
		{"unknown flag", []string{"--nope"}},
		{"bad level", []string{"--level", "bogus", "--", "bin"}},
		{"no binary and no package", []string{"--level", "basic"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			if code := run(context.Background(), c.args, &out, &errBuf); code != 2 {
				t.Fatalf("code = %d, want 2 (stderr=%q)", code, errBuf.String())
			}
		})
	}
}

func TestRun_MissingManifest(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run(context.Background(), []string{"--manifest", "does-not-exist.json", "--", "bin"}, &out, &errBuf)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !bytes.Contains(out.Bytes(), []byte("FAIL manifest")) {
		t.Fatalf("stdout %q does not report a manifest failure", out.String())
	}
}
