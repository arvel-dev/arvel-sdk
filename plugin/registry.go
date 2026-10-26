// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"sync"
	"time"
)

var errDuplicateSession = errors.New("plugin: duplicate session id")

// maxTombstones bounds the ids remembered after an idle close.
const maxTombstones = 1024

// turnState is the state of the running turn of a session.
type turnState struct {
	permCtx    context.Context // ends when the session is cancelled
	permCancel context.CancelFunc
	cancelled  bool    // session/cancel arrived during this turn
	watchdog   Stopper // armed by the first cancel
}

// sessionEntry is one live session. mu guards everything below it.
type sessionEntry struct {
	id     string
	sess   Session
	params SessionParams // as received; reused to reload after an idle close

	mu         sync.Mutex
	running    bool
	closing    bool
	turn       *turnState
	lastActive time.Time
}

// tryStart begins a turn. It fails while a turn runs or the session is closing.
func (e *sessionEntry) tryStart(parent context.Context) (*turnState, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running || e.closing {
		return nil, false
	}
	permCtx, cancel := context.WithCancel(parent)
	e.turn = &turnState{permCtx: permCtx, permCancel: cancel}
	e.running = true
	return e.turn, true
}

// finish ends the turn: it stops the watchdog and cancels pending permissions.
func (e *sessionEntry) finish(now time.Time) {
	e.mu.Lock()
	t := e.turn
	e.running, e.turn, e.lastActive = false, nil, now
	e.mu.Unlock()
	if t == nil {
		return
	}
	if t.watchdog != nil {
		t.watchdog.Stop()
	}
	t.permCancel()
}

func (e *sessionEntry) touch(now time.Time) {
	e.mu.Lock()
	e.lastActive = now
	e.mu.Unlock()
}

func (e *sessionEntry) isClosing() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closing
}

// reload is a LoadSession in flight for an idle-closed session; later callers
// wait for it instead of loading again.
type reload struct {
	done  chan struct{}
	entry *sessionEntry
	err   error
}

type registry struct {
	mu        sync.Mutex
	live      map[string]*sessionEntry
	expired   map[string]LoadParams // tombstones of idle-closed sessions
	order     []string              // insertion order of the tombstones
	reloading map[string]*reload
	maxTombs  int
}

func newRegistry() *registry {
	return &registry{
		live:      map[string]*sessionEntry{},
		expired:   map[string]LoadParams{},
		reloading: map[string]*reload{},
		maxTombs:  maxTombstones,
	}
}

func (r *registry) add(e *sessionEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.live[e.id]; ok {
		return errDuplicateSession
	}
	r.live[e.id] = e
	return nil
}

func (r *registry) get(id string) (*sessionEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.live[id]
	return e, ok
}

func (r *registry) all() []*sessionEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*sessionEntry, 0, len(r.live))
	for _, e := range r.live {
		out = append(out, e)
	}
	return out
}

// drain removes and returns every live session and forgets the tombstones.
func (r *registry) drain() []*sessionEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*sessionEntry, 0, len(r.live))
	for _, e := range r.live {
		out = append(out, e)
	}
	r.live = map[string]*sessionEntry{}
	r.expired = map[string]LoadParams{}
	r.order = nil
	return out
}

// expire moves a session from live to the tombstones in one step.
func (r *registry) expire(id string, lp LoadParams) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.live, id)
	if _, ok := r.expired[id]; !ok {
		r.order = append(r.order, id)
	}
	r.expired[id] = lp
	for len(r.order) > r.maxTombs {
		delete(r.expired, r.order[0])
		r.order = r.order[1:]
	}
}

func (r *registry) dropTombstone(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dropTombstoneLocked(id)
}

func (r *registry) dropTombstoneLocked(id string) {
	if _, ok := r.expired[id]; !ok {
		return
	}
	delete(r.expired, id)
	for i, o := range r.order {
		if o == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// resolution is what acquire has to do for an id.
type resolution struct {
	entry    *sessionEntry // a live session
	follow   *reload       // another caller is loading it
	leader   *reload       // this caller must load it
	loadWith LoadParams
}

// resolve decides, in one critical section, whether id is live, being
// reloaded, expired (this caller becomes the loader) or unknown.
func (r *registry) resolve(id string) (resolution, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.live[id]; ok {
		return resolution{entry: e}, true
	}
	if rl, ok := r.reloading[id]; ok {
		return resolution{follow: rl}, true
	}
	lp, ok := r.expired[id]
	if !ok {
		return resolution{}, false
	}
	rl := &reload{done: make(chan struct{})}
	r.reloading[id] = rl
	return resolution{leader: rl, loadWith: lp}, true
}

// finishReload publishes the outcome of a reload. On success the session goes
// live and the tombstone is dropped; on failure the tombstone is dropped too
// unless keepTombstone is set (the caller's context ended).
func (r *registry) finishReload(id string, rl *reload, e *sessionEntry, err error, keepTombstone bool) {
	r.mu.Lock()
	delete(r.reloading, id)
	if err == nil {
		r.live[id] = e
		r.dropTombstoneLocked(id)
	} else if !keepTombstone {
		r.dropTombstoneLocked(id)
	}
	rl.entry, rl.err = e, err
	r.mu.Unlock()
	close(rl.done)
}
