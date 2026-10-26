// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"io"
	"time"
)

// Defaults of the framework timers.
const (
	DefaultIdleTimeout  = 15 * time.Minute
	DefaultRouteTimeout = 2500 * time.Millisecond
	DefaultInitTimeout  = 5 * time.Second
	DefaultCloseTimeout = 5 * time.Second
	// UsageInterval is the shortest time between two _arvel/usage_updated messages.
	UsageInterval = 5 * time.Second
	// CancelWarnAfter is how long a turn may keep running after session/cancel
	// before the framework logs a warning.
	CancelWarnAfter = 10 * time.Second
)

type options struct {
	name, version string
	idle          time.Duration
	routeTimeout  time.Duration
	initTimeout   time.Duration
	closeTimeout  time.Duration
	in            io.Reader
	out           io.Writer
	stdio         bool // WithStdio was used: no signal handling
	stderr        io.Writer
	clock         Clock
	maxLine       int // 0 = jsonl.DefaultMaxLine; set by tests only
}

func defaultOptions() options {
	return options{
		name:         "arvel-plugin",
		version:      "0.0.0",
		idle:         DefaultIdleTimeout,
		routeTimeout: DefaultRouteTimeout,
		initTimeout:  DefaultInitTimeout,
		closeTimeout: DefaultCloseTimeout,
		clock:        realClock{},
	}
}

// Option configures Run and RunRouter.
type Option func(*options)

func positive(dst *time.Duration, d time.Duration) {
	if d > 0 {
		*dst = d
	}
}

// WithVersion sets the plugin name and version: the "plugin" attribute of every
// log line and the agentInfo of the initialize result.
func WithVersion(name, version string) Option {
	return func(o *options) {
		if name != "" {
			o.name = name
		}
		if version != "" {
			o.version = version
		}
	}
}

// WithIdleTimeout sets how long a session may be idle before it is closed
// (default 15 min). Non-positive values keep the default.
func WithIdleTimeout(d time.Duration) Option { return func(o *options) { positive(&o.idle, d) } }

// WithRouteTimeout sets the deadline of a route call (default 2.5 s).
func WithRouteTimeout(d time.Duration) Option {
	return func(o *options) { positive(&o.routeTimeout, d) }
}

// WithInitTimeout sets the deadline of Initialize (default 5 s).
func WithInitTimeout(d time.Duration) Option {
	return func(o *options) { positive(&o.initTimeout, d) }
}

// WithCloseTimeout sets the deadline for closing sessions, running turns at
// shutdown and Provider.Shutdown (default 5 s).
func WithCloseTimeout(d time.Duration) Option {
	return func(o *options) { positive(&o.closeTimeout, d) }
}

// WithStdio replaces stdin and stdout, for tests. It also turns off signal
// handling.
func WithStdio(r io.Reader, w io.Writer) Option {
	return func(o *options) { o.in, o.out, o.stdio = r, w, true }
}

// WithStderr replaces stderr as the log destination, for tests.
func WithStderr(w io.Writer) Option { return func(o *options) { o.stderr = w } }

// WithClock replaces the clock, for tests.
func WithClock(c Clock) Option {
	return func(o *options) {
		if c != nil {
			o.clock = c
		}
	}
}
