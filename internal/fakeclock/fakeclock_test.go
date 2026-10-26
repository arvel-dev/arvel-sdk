// SPDX-License-Identifier: Apache-2.0

package fakeclock

import (
	"slices"
	"testing"
	"time"
)

var start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestAdvanceFiresInOrder(t *testing.T) {
	c := New(start)
	var got []int
	c.AfterFunc(3*time.Second, func() { got = append(got, 3) })
	c.AfterFunc(1*time.Second, func() { got = append(got, 1) })
	c.AfterFunc(2*time.Second, func() { got = append(got, 2) })
	c.AfterFunc(10*time.Second, func() { got = append(got, 10) })
	c.Advance(5 * time.Second)
	if !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("fired %v", got)
	}
	if c.Pending() != 1 || !c.Now().Equal(start.Add(5*time.Second)) {
		t.Fatalf("pending %d, now %v", c.Pending(), c.Now())
	}
	c.Advance(5 * time.Second)
	if !slices.Equal(got, []int{1, 2, 3, 10}) || c.Pending() != 0 {
		t.Fatalf("fired %v, pending %d", got, c.Pending())
	}
}

func TestStopPreventsFire(t *testing.T) {
	c := New(start)
	fired := false
	tm := c.AfterFunc(time.Second, func() { fired = true })
	if !tm.Stop() {
		t.Fatal("Stop = false for a pending timer")
	}
	c.Advance(time.Minute)
	if fired || tm.Stop() {
		t.Fatalf("fired %v", fired)
	}
}

func TestCallbackMayReschedule(t *testing.T) {
	c := New(start)
	n := 0
	var tick func()
	tick = func() {
		n++
		c.AfterFunc(time.Second, tick)
	}
	c.AfterFunc(time.Second, tick)
	c.Advance(5 * time.Second)
	if n != 5 {
		t.Fatalf("ticks = %d, want 5", n)
	}
}
