// SPDX-License-Identifier: Apache-2.0

package fakevendor

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/arvel-dev/arvel-sdk/procx"
)

func TestBuild_RunAsSubprocess(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of a helper binary")
	}
	s, err := LoadFile("testdata/claude-basic-turn.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	bin, cleanup, err := Build(ctx, s, "fake-claude")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	t.Run("version", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		p, err := procx.Start(ctx, procx.Spec{Path: bin, Args: []string{"--version"}, Stdout: &out, Stderr: &errBuf})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { p.KillTree(0) })
		if code, err := p.Wait(); err != nil || code != 0 {
			t.Fatalf("version: code=%d err=%v stderr=%q", code, err, errBuf.String())
		}
		if got := strings.TrimSpace(out.String()); got != "2.1.310" {
			t.Fatalf("version = %q", got)
		}
	})

	t.Run("script", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		p, err := procx.Start(ctx, procx.Spec{
			Path:   bin,
			Stdin:  strings.NewReader(`{"type":"assistant","message":"hi"}` + "\n"),
			Stdout: &out,
			Stderr: &errBuf,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { p.KillTree(0) })
		if code, err := p.Wait(); err != nil || code != 0 {
			t.Fatalf("script: code=%d err=%v stderr=%q", code, err, errBuf.String())
		}
		got := out.String()
		if !strings.Contains(got, `"system"`) || !strings.Contains(got, `"result"`) {
			t.Fatalf("stdout = %q", got)
		}
	})
}
