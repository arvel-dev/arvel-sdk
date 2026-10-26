// SPDX-License-Identifier: Apache-2.0

//go:build unix

package procx

import (
	"context"
	"testing"
	"time"
)

func TestProc_InterruptSendsSIGINT(t *testing.T) {
	p, lr := startPiped(t, context.Background(), "sigint-exit")
	lr.waitFor(t, "ready")
	if err := p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	lr.waitFor(t, "got interrupt")
	code, err := p.Wait()
	if code != 0 || err != nil {
		t.Fatalf("Wait = %d, %v", code, err)
	}
}

func TestProc_KillTreeGraceful(t *testing.T) {
	p, lr := startPiped(t, context.Background(), "sleep")
	lr.waitFor(t, "ready")
	start := time.Now()
	p.KillTree(2 * time.Second)
	code, err := p.Wait()
	if code != 143 || err != nil {
		t.Fatalf("Wait = %d, %v; want 143 (SIGTERM)", code, err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestProc_KillTreeEscalatesToSIGKILL(t *testing.T) {
	p, lr := startPiped(t, context.Background(), "ignore-term")
	lr.waitFor(t, "ready")
	start := time.Now()
	p.KillTree(200 * time.Millisecond)
	code, err := p.Wait()
	elapsed := time.Since(start)
	if code != 137 || err != nil {
		t.Fatalf("Wait = %d, %v; want 137 (SIGKILL)", code, err)
	}
	if elapsed < 150*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("escalation took %v", elapsed)
	}
}

func TestProc_KillTreeZeroGraceIsImmediate(t *testing.T) {
	p, lr := startPiped(t, context.Background(), "ignore-term")
	lr.waitFor(t, "ready")
	start := time.Now()
	p.KillTree(0)
	code, err := p.Wait()
	if code != 137 || err != nil || time.Since(start) > 2*time.Second {
		t.Fatalf("Wait = %d, %v after %v", code, err, time.Since(start))
	}
}

func TestKillTree_GraceSignalsGrandchild(t *testing.T) {
	p, lr := startPiped(t, context.Background(), "spawn")
	gpid := lr.grandchildPID(t)
	lr.waitFor(t, "ready")
	start := time.Now()
	p.KillTree(5 * time.Second)
	waitDone(t, p)
	waitDead(t, gpid)
	if time.Since(start) > 4*time.Second {
		t.Fatalf("the group did not receive SIGTERM: took %v", time.Since(start))
	}
}
