// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/internal/fakeclock"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

type sent struct {
	at     time.Duration
	status protocol.UsageStatus
}

func newTestThrottle(clk *fakeclock.Clock, out *[]sent) *throttle {
	return &throttle{clk: clk, interval: UsageInterval, send: func(u protocol.Usage) {
		*out = append(*out, sent{clk.Now().Sub(testStart), u.Status})
	}}
}

func usageWith(s protocol.UsageStatus) protocol.Usage { return protocol.Usage{Status: s} }

func TestThrottle_FirstImmediateThenLastWins(t *testing.T) {
	clk := fakeclock.New(testStart)
	var out []sent
	th := newTestThrottle(clk, &out)

	th.Update(usageWith(protocol.UsageStatusOK)) // t=0: sent at once
	clk.Advance(time.Second)
	th.Update(usageWith(protocol.UsageStatusWarning)) // t=1s
	clk.Advance(time.Second)
	th.Update(usageWith(protocol.UsageStatusWarning)) // t=2s
	clk.Advance(time.Second)
	th.Update(usageWith(protocol.UsageStatusExhausted)) // t=3s: the last value
	clk.Advance(2 * time.Second)                        // t=5s: the interval ends

	want := []sent{{0, protocol.UsageStatusOK}, {5 * time.Second, protocol.UsageStatusExhausted}}
	if len(out) != 2 || out[0] != want[0] || out[1] != want[1] {
		t.Fatalf("sent %+v, want %+v", out, want)
	}
}

func TestThrottle_IdleSendsImmediately(t *testing.T) {
	clk := fakeclock.New(testStart)
	var out []sent
	th := newTestThrottle(clk, &out)
	th.Update(usageWith(protocol.UsageStatusOK))
	clk.Advance(6 * time.Second)
	th.Update(usageWith(protocol.UsageStatusWarning))
	if len(out) != 2 || out[1].at != 6*time.Second {
		t.Fatalf("sent %+v", out)
	}
}

func TestThrottle_FlushSendsPending(t *testing.T) {
	clk := fakeclock.New(testStart)
	var out []sent
	th := newTestThrottle(clk, &out)
	th.Update(usageWith(protocol.UsageStatusOK))
	th.Update(usageWith(protocol.UsageStatusWarning))
	th.Flush()
	if len(out) != 2 || out[1].status != protocol.UsageStatusWarning {
		t.Fatalf("sent %+v", out)
	}
	if clk.Pending() != 0 {
		t.Fatal("timer left running after Flush")
	}
	th.Flush() // nothing pending: no second send
	if len(out) != 2 {
		t.Fatalf("sent %+v", out)
	}
}

func TestThrottle_AtMostOnePerInterval(t *testing.T) {
	clk := fakeclock.New(testStart)
	var out []sent
	th := newTestThrottle(clk, &out)
	for range 100 {
		th.Update(usageWith(protocol.UsageStatusOK))
		clk.Advance(200 * time.Millisecond) // 20 s in total
	}
	if len(out) > 5 {
		t.Fatalf("%d sends in 20 s, want at most 5: %+v", len(out), out)
	}
	for i := 1; i < len(out); i++ {
		if out[i].at-out[i-1].at < UsageInterval {
			t.Fatalf("sends %d and %d are closer than the interval: %+v", i-1, i, out)
		}
	}
}

func TestThrottle_NoTimerLeak(t *testing.T) {
	clk := fakeclock.New(testStart)
	var out []sent
	th := newTestThrottle(clk, &out)
	th.Update(usageWith(protocol.UsageStatusOK))
	th.Update(usageWith(protocol.UsageStatusWarning))
	clk.Advance(time.Minute)
	if clk.Pending() != 0 {
		t.Fatalf("%d timers pending", clk.Pending())
	}
}

func TestStatusDebounce(t *testing.T) {
	var d statusDebounce
	a := protocol.Status{State: protocol.StateReady, Message: "ok"}
	if !d.Changed(a) {
		t.Fatal("the first status must pass")
	}
	if d.Changed(a) {
		t.Fatal("an identical status must be dropped")
	}
	b := a
	b.Message = "other"
	if !d.Changed(b) {
		t.Fatal("same state, different message must pass")
	}
	d.Seed(a)
	if d.Changed(a) {
		t.Fatal("a status seeded by _arvel/status must not be sent again")
	}
}
