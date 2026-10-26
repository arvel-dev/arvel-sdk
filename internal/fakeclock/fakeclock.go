// SPDX-License-Identifier: Apache-2.0

// Package fakeclock is a deterministic clock for tests. Time moves only when a
// test calls Advance, which runs due timers synchronously in the caller's
// goroutine. Clock satisfies plugin.Clock without importing the plugin package.
package fakeclock

import (
	"sync"
	"time"
)

// Clock is a manually advanced clock.
type Clock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*timer
}

type timer struct {
	c       *Clock
	at      time.Time
	f       func()
	done    bool // fired or stopped
	stopped bool
}

// New returns a Clock that starts at start.
func New(start time.Time) *Clock { return &Clock{now: start} }

// Now returns the current fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// AfterFunc schedules f to run when the clock has advanced by d.
func (c *Clock) AfterFunc(d time.Duration, f func()) interface{ Stop() bool } {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &timer{c: c, at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

// Stop prevents the timer from firing. It reports whether it had not fired yet.
func (t *timer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	if t.done {
		return false
	}
	t.done, t.stopped = true, true
	return true
}

// Pending returns the number of timers that have neither fired nor been stopped.
func (c *Clock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.done {
			n++
		}
	}
	return n
}

// Advance moves the clock forward by d, running every timer that becomes due,
// earliest first. A callback may schedule new timers, which run too when due.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	for {
		var next *timer
		for _, t := range c.timers {
			if !t.done && !t.at.After(target) && (next == nil || t.at.Before(next.at)) {
				next = t
			}
		}
		if next == nil {
			break
		}
		next.done = true
		if next.at.After(c.now) {
			c.now = next.at
		}
		c.mu.Unlock()
		next.f()
		c.mu.Lock()
	}
	c.now = target
	live := c.timers[:0:0]
	for _, t := range c.timers {
		if !t.done {
			live = append(live, t)
		}
	}
	c.timers = live
	c.mu.Unlock()
}
