// SPDX-License-Identifier: Apache-2.0

package leakcheck

import (
	"fmt"
	"testing"
)

// fakeT records a Fatalf instead of ending the real test.
type fakeT struct {
	testing.TB
	failed bool
	msg    string
}

func (f *fakeT) Helper() {}
func (f *fakeT) Fatalf(format string, args ...any) {
	f.failed = true
	f.msg = fmt.Sprintf(format, args...)
}

// leakedForTest must not be inlined, or its name would vanish from the stack.
//
//go:noinline
func leakedForTest(started, stop chan struct{}) {
	close(started) // the function is on the stack from here on
	<-stop
}

func TestCheck_PassesWhenClean(t *testing.T) {
	ft := &fakeT{}
	Check(ft, "leakedForTest")
	if ft.failed {
		t.Fatalf("unexpected failure: %s", ft.msg)
	}
}

func TestCheck_DetectsLeakedGoroutine(t *testing.T) {
	started, stop := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		leakedForTest(started, stop)
	}()
	<-started
	ft := &fakeT{}
	Check(ft, "leakedForTest")
	close(stop)
	<-done
	if !ft.failed {
		t.Fatal("Check did not detect the leaked goroutine")
	}
}
