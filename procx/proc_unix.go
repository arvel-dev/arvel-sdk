// SPDX-License-Identifier: Apache-2.0

//go:build unix

package procx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// platformTree is the per-OS state of a Proc: the process group the root leads.
type platformTree struct{ pgid int }

func newCommand(spec Spec, env []string) (*exec.Cmd, error) {
	return &exec.Cmd{
		Path:        spec.Path,
		Args:        append([]string{spec.Path}, spec.Args...),
		Dir:         spec.Dir,
		Env:         env,
		Stdin:       spec.Stdin,
		Stdout:      spec.Stdout,
		Stderr:      spec.Stderr,
		WaitDelay:   ioDrainDelay,
		SysProcAttr: &syscall.SysProcAttr{Setpgid: true}, // the root leads a new group: pgid == pid
	}, nil
}

func (p *Proc) startTree() error {
	if err := p.cmd.Start(); err != nil {
		return err
	}
	p.pid = p.cmd.Process.Pid
	p.pgid = p.pid // Setpgid made the root the group leader
	return nil
}

func exitCode(state *os.ProcessState) int {
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return state.ExitCode()
}

func (p *Proc) interrupt() error {
	err := syscall.Kill(-p.pgid, syscall.SIGINT)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	if err != nil {
		return fmt.Errorf("procx: interrupt: %w", err)
	}
	return nil
}

func (p *Proc) killTree(grace time.Duration) {
	pgid := p.pgid
	if grace > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		deadline := time.Now().Add(grace)
		for groupAlive(pgid) {
			if time.Now().After(deadline) {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	} else {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	select {
	case <-p.exited:
	case <-time.After(killWait):
	}
}

// groupAlive reports whether any process is left in the group. SIGKILL is sent
// only while the group is alive, never after it vanished (the id may be recycled).
func groupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
}

// cleanupTree kills what the root left behind, unless KillTree is handling it.
func (p *Proc) cleanupTree() {
	if !p.killing.Load() {
		_ = syscall.Kill(-p.pgid, syscall.SIGKILL)
	}
}
