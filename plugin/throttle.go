// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"sync"
	"time"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// throttle lets at most one usage update through per interval; the last value
// wins. The first update after a quiet interval is sent at once.
type throttle struct {
	clk      Clock
	interval time.Duration
	send     func(protocol.Usage)

	mu      sync.Mutex
	last    time.Time
	hasLast bool
	pending *protocol.Usage
	timer   Stopper
}

// Update sends u now, or keeps it for the end of the interval.
func (t *throttle) Update(u protocol.Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.clk.Now()
	if t.pending == nil && (!t.hasLast || now.Sub(t.last) >= t.interval) {
		t.last, t.hasLast = now, true
		t.send(u)
		return
	}
	t.pending = &u
	if t.timer == nil {
		t.timer = t.clk.AfterFunc(t.interval-now.Sub(t.last), t.fire)
	}
}

func (t *throttle) fire() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.timer = nil
	t.sendPending()
}

// Flush sends a pending value now and stops the timer.
func (t *throttle) Flush() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	t.sendPending()
}

func (t *throttle) sendPending() {
	if t.pending == nil {
		return
	}
	u := *t.pending
	t.pending = nil
	t.last, t.hasLast = t.clk.Now(), true
	t.send(u)
}
