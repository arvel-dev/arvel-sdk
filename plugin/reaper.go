// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// reaper closes idle sessions. It is a re-arming Clock timer, not a goroutine,
// so tests drive it with a fake clock.
type reaper struct {
	mu      sync.Mutex
	stopped bool
	timer   Stopper
	wg      sync.WaitGroup // ticks in flight
}

func reapInterval(idle time.Duration) time.Duration {
	return min(max(idle/4, time.Second), time.Minute)
}

func (s *providerServer) startReaper() {
	r := &s.reap
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.stopped {
		r.timer = s.o.clock.AfterFunc(reapInterval(s.o.idle), s.tick)
	}
}

func (s *providerServer) stopReaper() {
	r := &s.reap
	r.mu.Lock()
	r.stopped = true
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	r.mu.Unlock()
	r.wg.Wait()
}

func (s *providerServer) tick() {
	r := &s.reap
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.wg.Add(1)
	r.mu.Unlock()
	defer r.wg.Done()

	s.reapOnce()

	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.stopped {
		r.timer = s.o.clock.AfterFunc(reapInterval(s.o.idle), s.tick)
	}
}

// reapOnce closes every session that has been idle for the idle timeout and
// remembers how to load it again.
func (s *providerServer) reapOnce() {
	now := s.o.clock.Now()
	for _, e := range s.reg.all() {
		e.mu.Lock()
		if e.running || e.closing || now.Sub(e.lastActive) < s.o.idle {
			e.mu.Unlock()
			continue
		}
		e.closing = true
		// Moving the session to the tombstones while e.mu is held means no
		// request can find it "live" but closing (lock order: e.mu, then reg.mu).
		s.reg.expire(e.id, LoadParams{SessionID: e.id, SessionParams: e.params})
		e.mu.Unlock()
		s.closeOne(e)
	}
}

func sessionExpired() error {
	return protocol.NewError(protocol.ErrProviderError, "session expired", map[string]any{"vendorCode": "session_expired"})
}

// acquire returns the live session for id. An idle-closed session is loaded
// again through Provider.LoadSession, once even when several requests arrive at
// the same time.
func (s *providerServer) acquire(ctx context.Context, id string) (*sessionEntry, error) {
	res, ok := s.reg.resolve(id)
	switch {
	case !ok:
		return nil, invalidParams("unknown session %q", id)
	case res.entry != nil:
		return res.entry, nil
	case res.follow != nil:
		select {
		case <-res.follow.done:
			return res.follow.entry, res.follow.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	var e *sessionEntry
	var err error
	if !s.feats.LoadSession {
		err = sessionExpired()
	} else {
		e, err = s.reload(ctx, id, res.loadWith)
	}
	// The tombstone stays when the caller gave up or when resuming is not
	// supported at all (the answer is then always "session expired").
	keep := !s.feats.LoadSession || errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded)
	s.reg.finishReload(id, res.leader, e, err, keep)
	return e, err
}

func (s *providerServer) reload(ctx context.Context, id string, lp LoadParams) (e *sessionEntry, err error) {
	var sess Session
	s.safely(id, "Provider.LoadSession", func() { sess, err = s.p.LoadSession(ctx, lp) })
	if err != nil {
		s.log.Warn("reloading an idle session failed", "session", id, "err", s.redactor.String(err.Error()))
		return nil, sessionExpired()
	}
	if sess == nil || sess.ID() != id {
		s.log.Warn("reloading an idle session returned another session", "session", id)
		if sess != nil {
			s.closeOne(&sessionEntry{id: id, sess: sess})
		}
		return nil, sessionExpired()
	}
	return &sessionEntry{id: id, sess: sess, params: lp.SessionParams, lastActive: s.o.clock.Now()}, nil
}
