// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

const idle = 15 * time.Minute

func resumable(t *testing.T) *stubProvider { return withFeatures(t, Features{LoadSession: true}) }

func TestReaper_ClosesIdleSession(t *testing.T) {
	p := resumable(t)
	h := readyHarness(t, p, WithIdleTimeout(idle))
	h.newSession()
	h.clock.Advance(14 * time.Minute)
	if p.session(0).closeN.Load() != 0 {
		t.Fatal("closed before the idle timeout")
	}
	h.clock.Advance(2 * time.Minute)
	if p.session(0).closeN.Load() != 1 {
		t.Fatalf("closed %d times after the idle timeout", p.session(0).closeN.Load())
	}
}

func TestReaper_ActivityResetsIdle(t *testing.T) {
	p := resumable(t)
	h := readyHarness(t, p, WithIdleTimeout(idle))
	id := h.newSession()
	h.clock.Advance(10 * time.Minute)
	if _, err := h.prompt(id, "x"); err != nil {
		t.Fatal(err)
	}
	h.clock.Advance(14 * time.Minute) // 24 min since creation, 14 since the prompt
	if p.session(0).closeN.Load() != 0 {
		t.Fatal("closed although the session was active 14 minutes ago")
	}
	h.clock.Advance(2 * time.Minute)
	if p.session(0).closeN.Load() != 1 {
		t.Fatal("not closed 16 minutes after the last activity")
	}
}

func TestReaper_RunningTurnIsNeverReaped(t *testing.T) {
	p := resumable(t)
	wait, release := gate()
	started := make(chan struct{}, 1)
	sessionWithPrompt(p, func(context.Context, []acp.ContentBlock, Output) (TurnResult, error) {
		started <- struct{}{}
		wait()
		return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
	})
	h := readyHarness(t, p, WithIdleTimeout(idle))
	t.Cleanup(release)
	id := h.newSession()
	out := h.promptAsync(id, "x")
	recv(t, started, "turn start")
	h.clock.Advance(time.Hour)
	if p.session(0).closeN.Load() != 0 {
		t.Fatal("a session with a running turn was closed")
	}
	release()
	if res := recv(t, out, "prompt result"); res.err != nil {
		t.Fatal(res.err)
	}
	h.clock.Advance(14 * time.Minute) // the idle clock starts at the end of the turn
	if p.session(0).closeN.Load() != 0 {
		t.Fatal("closed 14 minutes after the turn ended")
	}
	h.clock.Advance(2 * time.Minute)
	if p.session(0).closeN.Load() != 1 {
		t.Fatal("not closed 16 minutes after the turn ended")
	}
}

func TestReaper_TransparentReload(t *testing.T) {
	p := resumable(t)
	h := readyHarness(t, p, WithIdleTimeout(idle))
	var res struct {
		SessionID string `json:"sessionId"`
	}
	params := map[string]any{"cwd": "/srv/w", "mcpServers": []any{},
		"_meta": map[string]any{"arvel": map[string]any{"model": "m1", "language": "ru"}}}
	if err := h.call(protocol.MethodSessionNew, params, &res); err != nil {
		t.Fatal(err)
	}
	h.clock.Advance(20 * time.Minute)
	if p.session(0).closeN.Load() != 1 {
		t.Fatal("not reaped")
	}
	if _, err := h.prompt(res.SessionID, "again"); err != nil {
		t.Fatalf("prompt on a reaped session: %v", err)
	}
	if p.loadN.Load() != 1 || p.lastLoad.SessionID != res.SessionID || p.lastLoad.CWD != "/srv/w" ||
		!reflect.DeepEqual(p.lastLoad.Meta, p.lastNew.Meta) {
		t.Fatalf("LoadSession got %+v, NewSession got %+v", p.lastLoad, p.lastNew)
	}
	if p.session(1).promptN.Load() != 1 {
		t.Fatal("the reloaded session did not run the prompt")
	}
}

func TestReaper_ReloadOnSetConfigOption(t *testing.T) {
	p := resumable(t)
	h := readyHarness(t, p, WithIdleTimeout(idle))
	id := h.newSession()
	h.clock.Advance(20 * time.Minute)
	err := h.call(protocol.MethodSessionSetConfigOption, map[string]any{"sessionId": id, "configId": "model", "value": "x"}, nil)
	if err != nil || p.loadN.Load() != 1 {
		t.Fatalf("err = %v, loads = %d", err, p.loadN.Load())
	}
}

func TestReaper_NoResumeSupport_SessionExpired(t *testing.T) {
	p := newStub(t) // LoadSession not supported
	h := readyHarness(t, p, WithIdleTimeout(idle))
	id := h.newSession()
	h.clock.Advance(20 * time.Minute)
	for range 2 { // the answer stays "session expired"
		_, err := h.prompt(id, "x")
		var re *protocol.RPCError
		if !errors.As(err, &re) {
			t.Fatal(err)
		}
		d, _ := re.Arvel()
		if d.Code != protocol.ErrProviderError || d.VendorCode != "session_expired" || d.Message != "session expired" {
			t.Fatalf("error data = %+v", d)
		}
	}
	if p.loadN.Load() != 0 {
		t.Fatal("LoadSession called although resume is not supported")
	}
}

func TestReaper_ReloadFailure_DropsTombstone(t *testing.T) {
	p := resumable(t)
	p.loadSession = func(context.Context, LoadParams) (Session, error) { return nil, errors.New("vendor lost it") }
	h := readyHarness(t, p, WithIdleTimeout(idle))
	id := h.newSession()
	h.clock.Advance(20 * time.Minute)
	if _, err := h.prompt(id, "x"); arvelCode(err) != protocol.ErrProviderError {
		t.Fatalf("first prompt: %v, want session expired", err)
	}
	if _, err := h.prompt(id, "x"); rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("second prompt: %v, want unknown session", err)
	}
}

func TestReaper_ReloadSingleFlight(t *testing.T) {
	for run := range 5 {
		t.Run("", func(t *testing.T) {
			p := resumable(t)
			wait, release := gate()
			entered := make(chan struct{}, 4)
			p.loadSession = func(_ context.Context, lp LoadParams) (Session, error) {
				entered <- struct{}{}
				wait()
				return p.makeSession(lp.SessionID), nil
			}
			h := readyHarness(t, p, WithIdleTimeout(idle))
			t.Cleanup(release)
			id := h.newSession()
			h.clock.Advance(20 * time.Minute)
			a := h.promptAsync(id, "a")
			recv(t, entered, "the reload to start")
			b := h.promptAsync(id, "b")
			time.Sleep(20 * time.Millisecond) // let b reach the wait
			release()
			for _, c := range []<-chan promptOutcome{a, b} {
				if out := recv(t, c, "prompt"); out.err != nil && arvelCode(out.err) != protocol.ErrBusy {
					t.Fatalf("run %d: %v", run, out.err)
				}
			}
			if p.loadN.Load() != 1 {
				t.Fatalf("run %d: LoadSession called %d times", run, p.loadN.Load())
			}
		})
	}
}

func TestRegistry_TombstoneCap(t *testing.T) {
	r := newRegistry()
	r.maxTombs = 2
	for _, id := range []string{"a", "b", "c"} {
		r.expire(id, LoadParams{SessionID: id})
	}
	if _, ok := r.resolve("a"); ok {
		t.Fatal("the oldest tombstone must be evicted")
	}
	if res, ok := r.resolve("c"); !ok || res.leader == nil {
		t.Fatalf("newest tombstone lost: %+v %v", res, ok)
	}
}

func TestReaper_CancelForReapedSessionIgnored(t *testing.T) {
	p := resumable(t)
	h := readyHarness(t, p, WithIdleTimeout(idle))
	id := h.newSession()
	h.clock.Advance(20 * time.Minute)
	h.cancelSession(id)
	if _, err := h.prompt("unknown", "x"); err == nil { // a request after the cancel: it was handled
		t.Fatal("want unknown session")
	}
	if p.loadN.Load() != 0 {
		t.Fatal("session/cancel must not reload a session")
	}
}

func TestReaper_StopsAtShutdown(t *testing.T) {
	p := resumable(t)
	h := readyHarness(t, p, WithIdleTimeout(idle))
	h.newSession()
	h.shutdown()
	if h.clock.Pending() != 0 {
		t.Fatalf("%d timers pending after shutdown", h.clock.Pending())
	}
	h.clock.Advance(time.Hour)
	if p.session(0).closeN.Load() != 1 {
		t.Fatalf("session closed %d times", p.session(0).closeN.Load())
	}
}

func TestReaper_CloseErrorLoggedAndSessionStillReloadable(t *testing.T) {
	p := resumable(t)
	var closes atomic.Int32
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		s := p.makeSession("")
		s.onClose = func(context.Context) error { closes.Add(1); return errors.New("close failed") }
		return s, nil
	}
	h := readyHarness(t, p, WithIdleTimeout(idle))
	id := h.newSession()
	h.clock.Advance(20 * time.Minute)
	if len(h.stderr.find(t, "session close failed")) != 1 {
		t.Fatalf("close failure not logged:\n%s", h.stderr.String())
	}
	if _, err := h.prompt(id, "x"); err != nil {
		t.Fatalf("reload after a failed close: %v", err)
	}
}

func TestReaper_ManySessionsOnlyIdleOnesClosed(t *testing.T) {
	p := resumable(t)
	h := readyHarness(t, p, WithIdleTimeout(idle))
	a := h.newSession()
	h.clock.Advance(10 * time.Minute)
	h.newSession()
	h.clock.Advance(7 * time.Minute) // a: 17 min idle, b: 7 min
	var closed sync.Map
	for i := range 2 {
		closed.Store(i, p.session(i).closeN.Load())
	}
	if v, _ := closed.Load(0); v != int32(1) {
		t.Fatalf("session %s should be closed: %v", a, v)
	}
	if v, _ := closed.Load(1); v != int32(0) {
		t.Fatalf("session 1 should stay open: %v", v)
	}
}
