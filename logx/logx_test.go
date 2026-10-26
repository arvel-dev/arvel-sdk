// SPDX-License-Identifier: Apache-2.0

package logx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/arvel-dev/arvel-sdk/redact"
)

const secret = "supersecret-value"

type secretStringer struct{}

func (secretStringer) String() string { return "stringer " + secret }

func newBuf(t *testing.T, lvl slog.Leveler, rd *redact.Redactor) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return NewWith("echo", Options{Writer: &buf, Level: lvl, Redactor: rd}), &buf
}

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("invalid JSON line %q: %v", l, err)
		}
		out = append(out, m)
	}
	return out
}

func TestNewWith_Format(t *testing.T) {
	l, buf := newBuf(t, slog.LevelDebug, nil)
	l.Debug("d")
	l.Info("hello", "session", "s1")
	l.Warn("w")
	l.Error("e")
	got := lines(t, buf)
	if len(got) != 4 {
		t.Fatalf("got %d lines", len(got))
	}
	for i, want := range []string{"debug", "info", "warn", "error"} {
		if got[i]["level"] != want {
			t.Errorf("line %d level = %v, want %s", i, got[i]["level"], want)
		}
		if _, ok := got[i]["time"]; ok {
			t.Errorf("line %d has a time key", i)
		}
		if _, ok := got[i]["LEVEL"]; ok {
			t.Errorf("line %d has an upper-case LEVEL key", i)
		}
	}
	m := got[1]
	ts, _ := m["ts"].(string)
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil || !strings.HasSuffix(ts, "Z") {
		t.Errorf("ts = %q (%v)", ts, err)
	}
	if m["msg"] != "hello" || m["plugin"] != "echo" || m["session"] != "s1" || m["pid"] != float64(os.Getpid()) {
		t.Errorf("line = %v", m)
	}
}

func TestLogger_RedactsMessageAndAttrs(t *testing.T) {
	rd := redact.New()
	rd.Add(secret)
	l, buf := newBuf(t, slog.LevelInfo, rd)
	l.Info("token "+secret, "k", secret, "g", slog.Group("inner", "x", secret),
		"err", errors.New("boom "+secret), "s", secretStringer{}, "m", map[string]string{"a": secret})
	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out, redact.Replacement) {
		t.Fatalf("no replacement in %s", out)
	}
}

func TestLogger_RedactsWithAttrsAndGroups(t *testing.T) {
	rd := redact.New()
	rd.Add(secret)
	l, buf := newBuf(t, slog.LevelInfo, rd)
	l.With("k", secret).WithGroup("g").Info("x", "y", secret)
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("secret leaked: %s", buf.String())
	}
}

func TestLogger_SecretAddedAfterCreationIsRedacted(t *testing.T) {
	rd := redact.New()
	l, buf := newBuf(t, slog.LevelInfo, rd)
	rd.Add(secret)
	l.Info("late " + secret)
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("secret leaked: %s", buf.String())
	}
}

func TestLogger_KeepsNonStringKinds(t *testing.T) {
	l, buf := newBuf(t, slog.LevelInfo, nil)
	l.Info("x", "n", 42, "b", true, "d", 1500*time.Millisecond, "f", 1.5, "nilerr", error(nil))
	m := lines(t, buf)[0]
	if m["n"] != float64(42) || m["b"] != true || m["f"] != 1.5 || m["d"] != float64(1500*time.Millisecond) {
		t.Errorf("line = %v", m)
	}
	if v, ok := m["nilerr"]; !ok || v != nil {
		t.Errorf("nilerr = %v (present %v)", v, ok)
	}
}

func TestNewWith_LevelFromEnv(t *testing.T) {
	t.Setenv(EnvLevel, "warn")
	l, buf := func() (*slog.Logger, *bytes.Buffer) {
		var b bytes.Buffer
		return NewWith("echo", Options{Writer: &b}), &b
	}()
	l.Info("dropped")
	l.Warn("kept")
	got := lines(t, buf)
	if len(got) != 1 || got[0]["msg"] != "kept" {
		t.Fatalf("lines = %v", got)
	}

	t.Setenv(EnvLevel, "DEBUG")
	var b bytes.Buffer
	NewWith("echo", Options{Writer: &b}).Debug("seen")
	if len(lines(t, &b)) != 1 {
		t.Fatal("DEBUG did not enable debug")
	}

	var b2 bytes.Buffer
	NewWith("echo", Options{Writer: &b2, Level: slog.LevelError}).Warn("dropped")
	if b2.Len() != 0 {
		t.Fatal("explicit Options.Level did not win over the environment")
	}
}

func TestNewWith_InvalidLevelWarns(t *testing.T) {
	t.Setenv(EnvLevel, "loud")
	var buf bytes.Buffer
	l := NewWith("echo", Options{Writer: &buf})
	l.Info("later")
	got := lines(t, &buf)
	if len(got) != 2 {
		t.Fatalf("lines = %v", got)
	}
	if got[0]["level"] != "warn" || !strings.Contains(got[0]["msg"].(string), "invalid ARVEL_LOG_LEVEL") || got[0]["value"] != "loud" {
		t.Errorf("warning line = %v", got[0])
	}
	if got[1]["msg"] != "later" {
		t.Errorf("info line = %v", got[1])
	}
}

func TestNew_WritesToStderr(t *testing.T) {
	t.Setenv(EnvLevel, "")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	New("echo").Info("hi")
	os.Stderr = old
	_ = w.Close()
	data, _ := io.ReadAll(r)
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &m); err != nil {
		t.Fatalf("stderr = %q: %v", data, err)
	}
	if m["plugin"] != "echo" || m["msg"] != "hi" {
		t.Errorf("line = %v", m)
	}
}

func TestParseLevel(t *testing.T) {
	ok := map[string]slog.Level{"debug": slog.LevelDebug, " INFO ": slog.LevelInfo, "warn": slog.LevelWarn, "warning": slog.LevelWarn, "error": slog.LevelError}
	for in, want := range ok {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "trace", "7"} {
		if _, err := ParseLevel(in); err == nil {
			t.Errorf("ParseLevel(%q) succeeded", in)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("short", 10); got != "short" {
		t.Errorf("short: %q", got)
	}
	if got := Truncate("12345", 5); got != "12345" {
		t.Errorf("exact: %q", got)
	}
	if got, want := Truncate(strings.Repeat("a", 20), 10), strings.Repeat("a", 10)+"…[truncated 10 bytes]"; got != want {
		t.Errorf("ascii: %q, want %q", got, want)
	}
	got := Truncate(strings.Repeat("é", 10), 5) // 20 bytes; cut at 5 would split a rune
	if !utf8.ValidString(got) || !strings.HasPrefix(got, "éé…[truncated 16 bytes]") {
		t.Errorf("utf8: %q", got)
	}
	long := strings.Repeat("z", 3000)
	if got := Truncate(long, 0); !strings.HasPrefix(got, strings.Repeat("z", DefaultTruncate)+"…") || strings.Count(got, "z") != DefaultTruncate {
		t.Errorf("default size: kept %d z's", strings.Count(got, "z"))
	}
	if got := Truncate("", 3); got != "" {
		t.Errorf("empty: %q", got)
	}
}
