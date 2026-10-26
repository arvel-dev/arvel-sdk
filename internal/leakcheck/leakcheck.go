// SPDX-License-Identifier: Apache-2.0

// Package leakcheck is a test helper that fails a test when goroutines of the
// named packages are still running. It replaces goleak without a dependency.
package leakcheck

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	pollEvery = 25 * time.Millisecond
	pollFor   = 2 * time.Second
)

// Check fails t when, after polling for up to 2 s, a goroutine other than the
// caller's still has one of the needles in its stack.
func Check(t testing.TB, needles ...string) {
	t.Helper()
	var offenders []string
	deadline := time.Now().Add(pollFor)
	for {
		offenders = offending(needles)
		if len(offenders) == 0 {
			return
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(pollEvery)
	}
	t.Fatalf("goroutines are still running:\n\n%s", strings.Join(offenders, "\n\n"))
}

func offending(needles []string) []string {
	var out []string
	for _, g := range dump() {
		// The first goroutine of the dump is the caller's own.
		if strings.Contains(g, "leakcheck.Check") || strings.Contains(g, "leakcheck.offending") {
			continue
		}
		for _, n := range needles {
			if strings.Contains(g, n) {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func dump() []string {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	return strings.Split(strings.TrimSpace(string(buf)), "\n\n")
}
