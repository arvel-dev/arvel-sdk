// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ioDrainDelay = 3 * time.Second        // exec.Cmd.WaitDelay: bound on waiting for pipes held by stray descendants
	cancelGrace  = 500 * time.Millisecond // KillTree grace used when the Start context is cancelled
	killWait     = 5 * time.Second        // how long KillTree waits for the root to be reaped after the kill
)

// Spec describes a program to start.
type Spec struct {
	// Path is the absolute path of the program (use FindBinary). It is never
	// resolved through PATH. A .cmd or .bat path runs through cmd.exe on Windows.
	Path string
	// Args are the arguments without argv[0].
	Args []string
	// Dir is the working directory; empty means the current directory.
	Dir string
	// Env is the environment as "NAME=value" strings. A nil Env means
	// Env().Build(), the minimal environment; a non-nil empty slice means an
	// empty environment. The parent environment is never inherited implicitly.
	Env []string
	// Stdin, Stdout and Stderr connect the standard streams; nil means no input
	// and discarded output.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// Proc is a started program and the root of its process tree.
//
// The root owns its tree. When the root exits, every descendant still in the
// tree is killed, on every OS, unless KillTree is running (it finishes the job
// itself and honours its grace period). Descendants that leave the tree (a
// daemon that calls setsid on Unix) are outside it.
type Proc struct {
	cmd      *exec.Cmd
	pid      int
	exited   chan struct{} // closed right after the root process is reaped
	done     chan struct{} // closed last: status stored, tree cleaned, goroutines finished
	exitCode int
	waitErr  error
	killOnce sync.Once
	killing  atomic.Bool
	platformTree // per-OS state (see proc_unix.go, proc_windows.go)
}

// Start runs the program described by spec as the root of a new process tree.
// It never uses a shell, except cmd.exe for .cmd and .bat files on Windows.
// Cancelling ctx kills the tree (SIGTERM, then SIGKILL after a short grace on
// Unix; immediately on Windows).
func Start(ctx context.Context, spec Spec) (*Proc, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("procx: start: %w", err)
	}
	if spec.Path == "" {
		return nil, errors.New("procx: empty path")
	}
	if !filepath.IsAbs(spec.Path) {
		return nil, fmt.Errorf("%w: %q", ErrRelativePath, spec.Path)
	}
	env := spec.Env
	if env == nil {
		env = Env().Build()
	}
	cmd, err := newCommand(spec, env)
	if err != nil {
		return nil, err
	}
	p := &Proc{cmd: cmd, exited: make(chan struct{}), done: make(chan struct{})}
	if err := p.startTree(); err != nil {
		return nil, fmt.Errorf("procx: start %s: %w", filepath.Base(spec.Path), err)
	}
	killed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(killed)
		p.onCancel()
	})
	go p.reap(stop, killed)
	return p, nil
}

func (p *Proc) onCancel() { p.KillTree(cancelGrace) }

// reap is the only goroutine of a Proc: it waits for the root, then joins the
// cancel callback, stores the status, cleans the tree and signals completion.
func (p *Proc) reap(stop func() bool, killed <-chan struct{}) {
	err := p.cmd.Wait()
	close(p.exited)
	if !stop() {
		<-killed // the cancel callback ran or is running: wait for it
	}
	p.exitCode, p.waitErr = exitStatus(p.cmd.ProcessState, err)
	p.cleanupTree()
	close(p.done)
}

func exitStatus(state *os.ProcessState, err error) (int, error) {
	if state == nil {
		return -1, fmt.Errorf("procx: wait: %w", err)
	}
	var ee *exec.ExitError
	if err == nil || errors.As(err, &ee) {
		return exitCode(state), nil // a non-zero exit is not an error
	}
	return exitCode(state), fmt.Errorf("procx: wait: %w", err)
}

// Pid returns the process id of the root.
func (p *Proc) Pid() int { return p.pid }

// Done returns a channel that is closed when the root has exited, its tree has
// been cleaned up and the exit status is available.
func (p *Proc) Done() <-chan struct{} { return p.done }

// Wait blocks until the root has exited and returns its exit code. A program
// killed by a signal on Unix reports 128+signal; one stopped by KillTree on
// Windows reports 1. The error is non-nil only when waiting itself failed,
// for example with exec.ErrWaitDelay. It is safe to call Wait many times and
// from many goroutines; every call returns the same values.
func (p *Proc) Wait() (exitCode int, err error) {
	<-p.done
	return p.exitCode, p.waitErr
}

// Interrupt asks the tree to stop gracefully: SIGINT to the process group on
// Unix. On Windows it returns ErrUnsupported (vendor plugins use their
// protocol-level interrupt). It returns os.ErrProcessDone after the root has
// exited.
func (p *Proc) Interrupt() error {
	select {
	case <-p.exited:
		return os.ErrProcessDone
	default:
	}
	return p.interrupt()
}

// KillTree stops the whole tree. On Unix it sends SIGTERM to the process
// group, waits up to grace for the group to disappear, then sends SIGKILL;
// grace <= 0 goes straight to SIGKILL. On Windows it terminates the Job Object
// at once and ignores grace. It returns after the root has been reaped (or
// after a safety timeout). KillTree is idempotent and safe for concurrent use:
// the first call decides the grace period, later calls wait for it, and a call
// after the process has finished does nothing.
func (p *Proc) KillTree(grace time.Duration) {
	select {
	case <-p.done:
		return
	default:
	}
	p.killOnce.Do(func() {
		p.killing.Store(true)
		p.killTree(grace)
	})
}
