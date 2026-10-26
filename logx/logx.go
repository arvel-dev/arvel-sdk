// SPDX-License-Identifier: Apache-2.0

// Package logx builds the logger of PROTOCOL §10: JSON lines on stderr with
// the keys ts, level, msg, plugin and pid, the level taken from
// ARVEL_LOG_LEVEL, and every string run through a redact.Redactor.
//
// Log call sites should pass strings and errors as attribute values. Other
// composite values are rendered with %+v and then redacted.
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/arvel-dev/arvel-sdk/redact"
)

// EnvLevel is the environment variable that selects the log level.
const EnvLevel = "ARVEL_LOG_LEVEL"

// Options tunes NewWith; every field is optional.
type Options struct {
	// Writer receives the log lines; default os.Stderr.
	Writer io.Writer
	// Level is the minimum level; default is ParseLevel of ARVEL_LOG_LEVEL, or info.
	Level slog.Leveler
	// Redactor scrubs every string; default is a redactor with patterns only.
	Redactor *redact.Redactor
}

// New returns the standard plugin logger: JSON on stderr, level from the
// environment, attributes plugin and pid.
func New(plugin string) *slog.Logger { return NewWith(plugin, Options{}) }

// NewWith is New with explicit options.
func NewWith(plugin string, o Options) *slog.Logger {
	var w io.Writer = os.Stderr
	if o.Writer != nil {
		w = o.Writer
	}
	rd := o.Redactor
	if rd == nil {
		rd = redact.New()
	}
	var lvl slog.Leveler = slog.LevelInfo
	badLevel := ""
	switch {
	case o.Level != nil:
		lvl = o.Level
	default:
		if raw := os.Getenv(EnvLevel); raw != "" {
			if l, err := ParseLevel(raw); err != nil {
				badLevel = raw
			} else {
				lvl = l
			}
		}
	}
	inner := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl, ReplaceAttr: replaceAttr})
	l := slog.New(&redactHandler{inner: inner, r: rd}).With("plugin", plugin, "pid", os.Getpid())
	if badLevel != "" {
		l.Warn("invalid ARVEL_LOG_LEVEL, using info", "value", badLevel)
	}
	return l
}

// ParseLevel converts debug, info, warn (or warning) and error, in any case,
// to a slog.Level.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return slog.LevelInfo, fmt.Errorf("logx: unknown log level %q", s)
}
