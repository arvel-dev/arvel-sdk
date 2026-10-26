// SPDX-License-Identifier: Apache-2.0

//go:build windows

package procx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writeShim writes a .cmd file with CRLF line endings that runs the test
// binary in the given helper mode and forwards its own arguments.
func writeShim(t *testing.T, mode string, extra ...string) string {
	t.Helper()
	body := []string{"@echo off", `"` + helperExe(t) + `" -test.run=^TestHelperProcess$ -- ` + mode + ` %*`}
	body = append(body, extra...)
	p := filepath.Join(t.TempDir(), "shim.cmd")
	if err := os.WriteFile(p, []byte(strings.Join(body, "\r\n")+"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProc_InterruptUnsupported(t *testing.T) {
	p, lr := startPiped(t, context.Background(), "sleep")
	lr.waitFor(t, "ready")
	err := p.Interrupt()
	if !errors.Is(err, ErrUnsupported) || !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Interrupt = %v", err)
	}
	p.KillTree(0)
}

func TestStart_CmdShim(t *testing.T) {
	args := []string{"plain", "two words", "a&b", "(x)", "semi;colon", "a=b", `C:\my dir\`}
	var out bytes.Buffer
	p, err := Start(context.Background(), Spec{Path: writeShim(t, "echo-args"), Args: args, Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if code, err := p.Wait(); code != 0 || err != nil {
		t.Fatalf("Wait = %d, %v", code, err)
	}
	var got []string
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		got = append(got, strings.TrimRight(l, "\r"))
	}
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("the program received %q, want %q", got, args)
	}
}

func TestStart_CmdShimRejectsUnsafeArgs(t *testing.T) {
	shim := writeShim(t, "echo-args")
	for _, a := range []string{"100%", `say "hi"`, "wow!"} {
		_, err := Start(context.Background(), Spec{Path: shim, Args: []string{a}})
		if !errors.Is(err, ErrUnsafeArg) {
			t.Errorf("argument %q: %v", a, err)
		}
	}
}

func TestStart_CmdShimExitCode(t *testing.T) {
	p, err := Start(context.Background(), Spec{Path: writeShim(t, "exit", "exit /b 7"), Args: []string{"0"}})
	if err != nil {
		t.Fatal(err)
	}
	if code, err := p.Wait(); code != 7 || err != nil {
		t.Fatalf("Wait = %d, %v; want 7", code, err)
	}
}

func TestStart_CmdShimTreeKill(t *testing.T) {
	pr, pw := pipePair(t)
	p, err := Start(context.Background(), Spec{Path: writeShim(t, "spawn"), Stdout: pw})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.KillTree(0) })
	lr := newLineReader(pr)
	gpid := lr.grandchildPID(t)
	lr.waitFor(t, "ready")
	p.KillTree(0)
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("tree did not stop")
	}
	waitDead(t, p.Pid())
	waitDead(t, gpid)
}
