// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestHelperProcess is not a test: the test binary re-executes itself with
// "-test.run=^TestHelperProcess$ -- <mode> <args...>" to play a child program.
// Run normally (no "--" argument) it returns at once.
func TestHelperProcess(t *testing.T) {
	i := -1
	for j, a := range os.Args {
		if a == "--" {
			i = j
			break
		}
	}
	if i < 0 || i+1 >= len(os.Args) {
		return
	}
	runHelper(os.Args[i+1], os.Args[i+2:])
	os.Exit(0)
}

func sleepLong() { time.Sleep(2 * time.Minute) } // a leaked helper ends by itself

func runHelper(mode string, rest []string) {
	switch mode {
	case "exit":
		code, _ := strconv.Atoi(rest[0])
		os.Exit(code)
	case "echo-args":
		for _, a := range rest {
			fmt.Println(a)
		}
	case "stdin-echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "print-env":
		for _, e := range os.Environ() {
			fmt.Println(e)
		}
	case "stderr":
		fmt.Fprint(os.Stderr, rest[0])
	case "sleep":
		fmt.Println("ready")
		sleepLong()
	case "spawn", "spawn-exit":
		exe, _ := os.Executable()
		c := exec.Command(exe, helperArgs("sleep")...)
		if err := c.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "spawn:", err)
			os.Exit(3)
		}
		fmt.Println("grandchild", c.Process.Pid)
		if mode == "spawn-exit" {
			return
		}
		fmt.Println("ready")
		sleepLong()
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		fmt.Println("ready")
		sleepLong()
	case "sigint-exit":
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		fmt.Println("ready")
		<-ch
		fmt.Println("got interrupt")
	case "version":
		fmt.Println(rest[0])
	case "version-stderr":
		fmt.Fprintln(os.Stderr, rest[0])
	case "version-exit":
		fmt.Println(rest[0])
		code, _ := strconv.Atoi(rest[1])
		os.Exit(code)
	case "flood-version":
		fmt.Println(rest[0])
		chunk := []byte(strings.Repeat("x", 1024))
		for i := 0; i < 1024; i++ {
			_, _ = os.Stdout.Write(chunk)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown helper mode", mode)
		os.Exit(2)
	}
}

func helperArgs(mode string, rest ...string) []string {
	return append([]string{"-test.run=^TestHelperProcess$", "--", mode}, rest...)
}

func helperExe(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

// startHelper starts the helper in the given mode with the minimal environment
// and registers a cleanup that kills its tree.
func startHelper(t *testing.T, ctx context.Context, stdout, stderr io.Writer, mode string, rest ...string) *Proc {
	t.Helper()
	p, err := Start(ctx, Spec{
		Path: helperExe(t), Args: helperArgs(mode, rest...), Env: Env().Build(),
		Stdout: stdout, Stderr: stderr,
	})
	if err != nil {
		t.Fatalf("start helper %s: %v", mode, err)
	}
	t.Cleanup(func() { p.KillTree(0) })
	return p
}

// lineReader reads the lines of a helper's stdout in a goroutine so tests can
// wait for a line with a timeout.
type lineReader struct{ ch chan string }

func newLineReader(r io.Reader) *lineReader {
	lr := &lineReader{ch: make(chan string, 64)}
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			lr.ch <- strings.TrimSpace(sc.Text())
		}
		close(lr.ch)
	}()
	return lr
}

func (lr *lineReader) next(t *testing.T) string {
	t.Helper()
	select {
	case s, ok := <-lr.ch:
		if !ok {
			t.Fatal("helper output closed")
		}
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for helper output")
	}
	return ""
}

func (lr *lineReader) waitFor(t *testing.T, want string) {
	t.Helper()
	for {
		if lr.next(t) == want {
			return
		}
	}
}

func (lr *lineReader) grandchildPID(t *testing.T) int {
	t.Helper()
	line := lr.next(t)
	pid, err := strconv.Atoi(strings.TrimPrefix(line, "grandchild "))
	if err != nil || !strings.HasPrefix(line, "grandchild ") {
		t.Fatalf("unexpected line %q, want \"grandchild <pid>\"", line)
	}
	return pid
}

func pipePair(t *testing.T) (*io.PipeReader, *io.PipeWriter) {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pr.Close() }) // unblocks a writer nobody reads any more
	return pr, pw
}

// startPiped starts a helper whose stdout is readable through a lineReader.
func startPiped(t *testing.T, ctx context.Context, mode string, rest ...string) (*Proc, *lineReader) {
	t.Helper()
	pr, pw := pipePair(t)
	p := startHelper(t, ctx, pw, nil, mode, rest...)
	return p, newLineReader(pr)
}

func waitDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("process %d is still alive", pid)
}

func waitDone(t *testing.T, p *Proc) {
	t.Helper()
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("process did not finish")
	}
}
