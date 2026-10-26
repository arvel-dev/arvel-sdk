// SPDX-License-Identifier: Apache-2.0

package plugin

import "time"

// Stopper stops a timer started by Clock.AfterFunc. It is an alias of an
// anonymous interface so test clocks can satisfy Clock without importing this
// package.
type Stopper = interface{ Stop() bool }

// Clock abstracts time for the throttle, the cancel watchdog and the idle
// reaper, so tests can drive them without sleeping.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Stopper
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) Stopper { return time.AfterFunc(d, f) }
