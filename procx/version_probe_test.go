// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func probe(t *testing.T, mode string, rest ...string) (string, string, error) {
	t.Helper()
	return Version(context.Background(), helperExe(t), helperArgs(mode, rest...)...)
}

func TestVersion_ParsesStdout(t *testing.T) {
	ver, raw, err := probe(t, "version", "v2.1.310 (Claude Code)")
	if err != nil || ver != "2.1.310" || !strings.Contains(raw, "Claude Code") {
		t.Fatalf("got %q, %q, %v", ver, raw, err)
	}
}

func TestVersion_ParsesStderr(t *testing.T) {
	ver, _, err := probe(t, "version-stderr", "codex-cli 0.46.0")
	if err != nil || ver != "0.46.0" {
		t.Fatalf("got %q, %v", ver, err)
	}
}

func TestVersion_PreRelease(t *testing.T) {
	ver, _, err := probe(t, "version", "tool 1.2.3-beta.1")
	if err != nil || ver != "1.2.3-beta.1" {
		t.Fatalf("got %q, %v", ver, err)
	}
}

func TestVersion_NoVersion(t *testing.T) {
	ver, raw, err := probe(t, "version", "hello")
	// A coverage-instrumented child may append its own GOCOVERDIR warning.
	if !errors.Is(err, ErrNoVersion) || ver != "" || !strings.HasPrefix(raw, "hello") {
		t.Fatalf("got %q, %q, %v", ver, raw, err)
	}
}

func TestVersion_NonZeroExit(t *testing.T) {
	ver, raw, err := probe(t, "version-exit", "tool 1.2.3", "2")
	if err == nil || ver != "" || !strings.Contains(raw, "1.2.3") {
		t.Fatalf("got %q, %q, %v", ver, raw, err)
	}
}

func TestVersion_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := Version(ctx, helperExe(t), helperArgs("sleep")...)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestVersion_FloodedOutput(t *testing.T) {
	start := time.Now()
	ver, raw, err := probe(t, "flood-version", "tool 3.4.5")
	if err != nil || ver != "3.4.5" {
		t.Fatalf("got %q, %v", ver, err)
	}
	if len(raw) > versionRawLimit {
		t.Fatalf("raw is %d bytes", len(raw))
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestVersion_StartError(t *testing.T) {
	_, _, err := Version(context.Background(), helperExe(t)+".does-not-exist")
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("err = %v", err)
	}
}

func TestVersionArgs(t *testing.T) {
	if got := versionArgs(nil); !reflect.DeepEqual(got, []string{"--version"}) {
		t.Errorf("default = %v", got)
	}
	if got := versionArgs([]string{"-v"}); !reflect.DeepEqual(got, []string{"-v"}) {
		t.Errorf("explicit = %v", got)
	}
}

func TestLimitedBuffer(t *testing.T) {
	b := &limitedBuffer{max: 5}
	for _, s := range []string{"abc", "defgh", "ij"} {
		if n, err := b.Write([]byte(s)); n != len(s) || err != nil {
			t.Fatalf("Write(%q) = %d, %v", s, n, err)
		}
	}
	if b.String() != "abcde" {
		t.Fatalf("kept %q", b.String())
	}
}

func TestJoinOutput(t *testing.T) {
	if got := joinOutput(" a \n", "\nb\n"); got != "a\nb" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("é", versionRawLimit) // 2 bytes each
	if got := joinOutput(long, ""); len(got) > versionRawLimit || !strings.HasSuffix(got, "é") {
		t.Errorf("not cut at a rune boundary: len=%d", len(got))
	}
}
