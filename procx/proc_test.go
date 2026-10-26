// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStart_ExitCode(t *testing.T) {
	p := startHelper(t, context.Background(), nil, nil, "exit", "3")
	var wg sync.WaitGroup
	results := make([]int, 4)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, err := p.Wait()
			if err != nil {
				t.Errorf("Wait: %v", err)
			}
			results[i] = code
		}()
	}
	wg.Wait()
	for _, c := range results {
		if c != 3 {
			t.Fatalf("exit codes = %v, want all 3", results)
		}
	}
}

func TestStart_RejectsBadPath(t *testing.T) {
	ctx := context.Background()
	if _, err := Start(ctx, Spec{}); err == nil {
		t.Error("empty path accepted")
	}
	if _, err := Start(ctx, Spec{Path: "relative"}); !errors.Is(err, ErrRelativePath) {
		t.Errorf("relative path: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Start(cctx, Spec{Path: helperExe(t)}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context: %v", err)
	}
	missing := helperExe(t) + ".does-not-exist"
	_, err := Start(ctx, Spec{Path: missing, Args: []string{"--token=hunter2hunter2"}})
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("missing program: %v", err)
	}
}

func TestStart_StdioWiring(t *testing.T) {
	var out, errOut bytes.Buffer
	p, err := Start(context.Background(), Spec{
		Path: helperExe(t), Args: helperArgs("stdin-echo"), Env: Env().Build(),
		Stdin: strings.NewReader("hello\n"), Stdout: &out, Stderr: &errOut,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, err := p.Wait(); code != 0 || err != nil {
		t.Fatalf("Wait = %d, %v", code, err)
	}
	if strings.TrimSpace(out.String()) != "hello" {
		t.Fatalf("stdout = %q", out.String())
	}

	var e bytes.Buffer
	p = startHelper(t, context.Background(), nil, &e, "stderr", "oops")
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	// A coverage-instrumented child may append its own GOCOVERDIR warning.
	if !strings.HasPrefix(e.String(), "oops") {
		t.Fatalf("stderr = %q", e.String())
	}
}

func TestStart_NilEnvIsMinimal(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "x")
	t.Setenv("PROCX_TEST_SECRET", "y")
	var out bytes.Buffer
	p, err := Start(context.Background(), Spec{Path: helperExe(t), Args: helperArgs("print-env"), Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	got := parse(strings.Fields(strings.ReplaceAll(out.String(), "\r", "")))
	if _, ok := got["GITHUB_TOKEN"]; ok {
		t.Error("GITHUB_TOKEN inherited")
	}
	if _, ok := got["PROCX_TEST_SECRET"]; ok {
		t.Error("PROCX_TEST_SECRET inherited")
	}
	if got["TERM"] != "dumb" {
		t.Errorf("TERM = %q", got["TERM"])
	}
}

func TestStart_ExplicitEnvIsUsedAsIs(t *testing.T) {
	var out bytes.Buffer
	p, err := Start(context.Background(), Spec{
		Path: helperExe(t), Args: helperArgs("print-env"),
		Env: []string{"PROCX_ONLY=1"}, Stdout: &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(out.String(), "\r", "")
	if !strings.Contains(text, "PROCX_ONLY=1") {
		t.Fatalf("variable missing: %q", text)
	}
	if strings.Contains(strings.ToUpper(text), "PATH=") || strings.Contains(text, "TERM=") {
		t.Fatalf("parent environment leaked: %q", text)
	}
	if runtime.GOOS != "windows" && strings.TrimSpace(text) != "PROCX_ONLY=1" {
		t.Fatalf("environment = %q", text)
	}
}

func TestProc_InterruptAfterExit(t *testing.T) {
	p := startHelper(t, context.Background(), nil, nil, "exit", "0")
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := p.Interrupt(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("Interrupt after exit = %v", err)
	}
}

func TestProc_KillTreeIdempotent(t *testing.T) {
	p, lr := startPiped(t, context.Background(), "sleep")
	lr.waitFor(t, "ready")
	p.KillTree(100 * time.Millisecond)
	p.KillTree(100 * time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.KillTree(100 * time.Millisecond)
		}()
	}
	wg.Wait()
	waitDone(t, p)
	p.KillTree(0) // after Wait: a no-op
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestKillTree_LeavesNoSurvivors(t *testing.T) {
	p, lr := startPiped(t, context.Background(), "spawn")
	gpid := lr.grandchildPID(t)
	lr.waitFor(t, "ready")
	if !processAlive(p.Pid()) || !processAlive(gpid) {
		t.Fatalf("tree not running: root=%d grandchild=%d", p.Pid(), gpid)
	}
	p.KillTree(time.Second)
	waitDone(t, p)
	waitDead(t, p.Pid())
	waitDead(t, gpid)
}

func TestProc_ContextCancelKillsTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, lr := startPiped(t, ctx, "spawn")
	gpid := lr.grandchildPID(t)
	lr.waitFor(t, "ready")
	cancel()
	waitDone(t, p)
	code, err := p.Wait()
	if err != nil || code == 0 {
		t.Fatalf("Wait = %d, %v; want a non-zero code after cancel", code, err)
	}
	waitDead(t, p.Pid())
	waitDead(t, gpid)
}

func TestProc_RootExitKillsStragglers(t *testing.T) {
	p, lr := startPiped(t, context.Background(), "spawn-exit")
	gpid := lr.grandchildPID(t)
	if code, err := p.Wait(); code != 0 || err != nil {
		t.Fatalf("Wait = %d, %v", code, err)
	}
	waitDead(t, gpid)
}

func TestProc_KillTreeWhileWaiting(t *testing.T) {
	p, lr := startPiped(t, context.Background(), "sleep")
	lr.waitFor(t, "ready")
	finished := make(chan struct{})
	go func() {
		_, _ = p.Wait()
		close(finished)
	}()
	p.KillTree(0)
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return after KillTree")
	}
}

func TestProc_NoGoroutineLeak(t *testing.T) {
	// Run to completion.
	p := startHelper(t, context.Background(), nil, nil, "exit", "0")
	_, _ = p.Wait()
	// Cancel the context.
	ctx, cancel := context.WithCancel(context.Background())
	p, lr := startPiped(t, ctx, "sleep")
	lr.waitFor(t, "ready")
	cancel()
	_, _ = p.Wait()
	// Explicit kill.
	p, lr = startPiped(t, context.Background(), "sleep")
	lr.waitFor(t, "ready")
	p.KillTree(0)
	_, _ = p.Wait()

	var dump string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		buf := make([]byte, 1<<20)
		dump = string(buf[:runtime.Stack(buf, true)])
		if !strings.Contains(dump, "(*Proc).reap") && !strings.Contains(dump, "(*Proc).onCancel") {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("goroutines of procx.Proc are still running:\n%s", dump)
}
