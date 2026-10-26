// SPDX-License-Identifier: Apache-2.0

//go:build windows

package procx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// platformTree holds the Job Object that contains the whole tree.
type platformTree struct {
	mu  sync.Mutex
	job windows.Handle
}

func newCommand(spec Spec, env []string) (*exec.Cmd, error) {
	cmd := &exec.Cmd{
		Path:      spec.Path,
		Args:      append([]string{spec.Path}, spec.Args...),
		Dir:       spec.Dir,
		Env:       env,
		Stdin:     spec.Stdin,
		Stdout:    spec.Stdout,
		Stderr:    spec.Stderr,
		WaitDelay: ioDrainDelay,
	}
	// Suspended so the root joins the job before it runs; a new process group
	// isolates it from console Ctrl+C aimed at the plugin; no window for
	// console programs when the plugin itself has no console.
	attr := &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
	}
	if isCmdShim(spec.Path) {
		cmdExe, err := resolveCmdExe(env)
		if err != nil {
			return nil, err
		}
		line, err := cmdShimLine(cmdExe, spec.Path, spec.Args)
		if err != nil {
			return nil, err
		}
		cmd.Path, cmd.Args, attr.CmdLine = cmdExe, []string{cmdExe}, line
	}
	cmd.SysProcAttr = attr
	return cmd, nil
}

// resolveCmdExe finds cmd.exe through SystemRoot (from env, then the process
// environment), and falls back to PATH.
func resolveCmdExe(env []string) (string, error) {
	root := ""
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && strings.EqualFold(k, "SystemRoot") {
			root = v
		}
	}
	if root == "" {
		root = os.Getenv("SystemRoot")
	}
	if root != "" {
		p := filepath.Join(root, "System32", "cmd.exe")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	p, err := exec.LookPath("cmd.exe")
	if err != nil {
		return "", fmt.Errorf("procx: cmd.exe not found: %w", err)
	}
	return p, nil
}

func (p *Proc) startTree() error {
	job, err := newKillOnCloseJob()
	if err != nil {
		return err
	}
	if err := p.cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	p.pid = p.cmd.Process.Pid
	if err := assignAndResume(job, uint32(p.pid)); err != nil {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
		_ = windows.CloseHandle(job)
		return err
	}
	p.job = job
	return nil
}

func newKillOnCloseJob() (windows.Handle, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("procx: job object: %w", err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	runtime.KeepAlive(&info)
	if err != nil {
		_ = windows.CloseHandle(h)
		return 0, fmt.Errorf("procx: job object: %w", err)
	}
	return h, nil
}

func assignAndResume(job windows.Handle, pid uint32) error {
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return fmt.Errorf("procx: open process: %w", err)
	}
	defer windows.CloseHandle(ph)
	if err := windows.AssignProcessToJobObject(job, ph); err != nil {
		return fmt.Errorf("procx: assign to job: %w", err)
	}
	return resumeThreads(pid)
}

// resumeThreads resumes the threads of a process created suspended. A new
// process has exactly one thread, so exactly one is resumed.
func resumeThreads(pid uint32) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("procx: thread snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)
	te := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	err = windows.Thread32First(snap, &te)
	resumed := 0
	for err == nil {
		if te.OwnerProcessID == pid {
			th, oerr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
			if oerr != nil {
				return fmt.Errorf("procx: open thread: %w", oerr)
			}
			_, rerr := windows.ResumeThread(th)
			_ = windows.CloseHandle(th)
			if rerr != nil {
				return fmt.Errorf("procx: resume thread: %w", rerr)
			}
			resumed++
		}
		err = windows.Thread32Next(snap, &te)
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("procx: enumerate threads: %w", err)
	}
	if resumed == 0 {
		return errors.New("procx: no thread to resume")
	}
	return nil
}

func exitCode(state *os.ProcessState) int { return state.ExitCode() }

func (p *Proc) interrupt() error { return ErrUnsupported }

// killTree terminates the job at once; grace does not apply on Windows.
func (p *Proc) killTree(_ time.Duration) {
	p.mu.Lock()
	if p.job != 0 {
		_ = windows.TerminateJobObject(p.job, 1)
	}
	p.mu.Unlock()
	select {
	case <-p.exited:
	case <-time.After(killWait):
	}
}

// cleanupTree closes the job; KILL_ON_JOB_CLOSE kills whatever is left.
func (p *Proc) cleanupTree() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.job != 0 {
		_ = windows.CloseHandle(p.job)
		p.job = 0
	}
}
