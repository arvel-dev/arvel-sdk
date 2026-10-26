// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// update is one session/update the client received.
type update struct {
	SessionID string         `json:"sessionId"`
	Meta      map[string]any `json:"_meta"`
	Update    struct {
		Kind    string `json:"sessionUpdate"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"update"`
}

type updateLog struct {
	mu   sync.Mutex
	list []update
}

func (u *updateLog) texts() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]string, len(u.list))
	for i, x := range u.list {
		out[i] = x.Update.Content.Text
	}
	return out
}

func (u *updateLog) snapshot() []update {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]update(nil), u.list...)
}

func (h *harness) collectUpdates() *updateLog {
	l := &updateLog{}
	h.client.HandleNotification(protocol.MethodSessionUpdate, func(_ context.Context, raw json.RawMessage) {
		var u update
		if err := json.Unmarshal(raw, &u); err != nil {
			h.t.Errorf("bad session/update: %v", err)
			return
		}
		l.mu.Lock()
		l.list = append(l.list, u)
		l.mu.Unlock()
	})
	return l
}

// sessionWithPrompt makes the provider create sessions whose Prompt is f.
func sessionWithPrompt(p *stubProvider, f func(ctx context.Context, blocks []acp.ContentBlock, out Output) (TurnResult, error)) {
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		s := p.makeSession("")
		s.prompt = f
		return s, nil
	}
}

func TestPrompt_StreamsUpdatesBeforeResponse(t *testing.T) {
	p := newStub(t)
	turnMeta := goldenAs[protocol.TurnMeta](t, "7.2-turn-meta.json", "/_meta/arvel")
	sessionWithPrompt(p, func(_ context.Context, _ []acp.ContentBlock, out Output) (TurnResult, error) {
		for i := range 20 {
			var meta *protocol.UpdateMeta
			if i%2 == 1 {
				meta = &protocol.UpdateMeta{Subagent: &protocol.SubagentMeta{ID: "sub1", ParentToolCallID: "t1", Label: "Explore"}}
			}
			if err := out.Update(acp.UpdateAgentMessageText(fmt.Sprintf("w%d", i)), meta); err != nil {
				return TurnResult{}, err
			}
		}
		return TurnResult{StopReason: acp.StopReasonEndTurn, Usage: turnMeta.Usage}, nil
	})
	for run := range 10 {
		t.Run(fmt.Sprintf("run%d", run), func(t *testing.T) {
			h := readyHarness(t, p)
			log := h.collectUpdates()
			id := h.newSession()
			res, err := h.prompt(id, "hi")
			if err != nil {
				t.Fatalf("run %d: %v", run, err)
			}
			got := log.snapshot()
			if len(got) != 20 {
				t.Fatalf("run %d: Call returned after %d of 20 updates", run, len(got))
			}
			for i, u := range got {
				if u.Update.Content.Text != fmt.Sprintf("w%d", i) || u.SessionID != id {
					t.Fatalf("run %d: update %d = %+v", run, i, u)
				}
				_, hasMeta := u.Meta["arvel"]
				if hasMeta != (i%2 == 1) {
					t.Fatalf("run %d: update %d meta = %v", run, i, u.Meta)
				}
			}
			if res.StopReason != "end_turn" {
				t.Fatalf("stopReason = %q", res.StopReason)
			}
			mustEqualJSON(t, golden(t, "7.2-turn-meta.json", "/_meta"), res.Meta)
			h.shutdown()
		})
	}
}

func TestPrompt_Busy(t *testing.T) {
	p := newStub(t)
	wait, release := gate()
	started := make(chan struct{}, 1)
	var runs atomic.Int32
	sessionWithPrompt(p, func(context.Context, []acp.ContentBlock, Output) (TurnResult, error) {
		if runs.Add(1) == 1 {
			started <- struct{}{}
			wait()
		}
		return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
	})
	h := readyHarness(t, p)
	t.Cleanup(release)
	id := h.newSession()
	first := h.promptAsync(id, "one")
	recv(t, started, "first prompt to start")

	_, err := h.prompt(id, "two")
	if rpcCode(err) != protocol.CodeBusy || arvelCode(err) != protocol.ErrBusy {
		t.Fatalf("second prompt: %v, want busy -32002", err)
	}
	release()
	if out := recv(t, first, "first prompt"); out.err != nil || out.res.StopReason != "end_turn" {
		t.Fatalf("first prompt: %+v", out)
	}
	if _, err := h.prompt(id, "three"); err != nil {
		t.Fatalf("a prompt after the turn ended: %v", err)
	}
}

func TestPrompt_InvalidRequests(t *testing.T) {
	h := readyHarness(t, newStub(t))
	id := h.newSession()
	if _, err := h.prompt("nope", "x"); rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("unknown session: %v", err)
	}
	err := h.call(protocol.MethodSessionPrompt, map[string]any{"sessionId": id}, nil)
	if rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("missing prompt array: %v", err)
	}
}

func TestPrompt_InvalidProviderOutput(t *testing.T) {
	tests := []struct {
		name string
		res  TurnResult
	}{
		{"stop_reason", TurnResult{StopReason: "whatever"}},
		{"empty_stop_reason", TurnResult{}},
		{"stop_detail", TurnResult{StopReason: acp.StopReasonEndTurn, Stop: &protocol.StopDetail{Code: protocol.ErrProviderError}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newStub(t)
			sessionWithPrompt(p, func(context.Context, []acp.ContentBlock, Output) (TurnResult, error) { return tc.res, nil })
			h := readyHarness(t, p)
			_, err := h.prompt(h.newSession(), "x")
			if arvelCode(err) != protocol.ErrInternal {
				t.Fatalf("err = %v, want internal", err)
			}
		})
	}
}

func TestPrompt_ProviderErrorTyped(t *testing.T) {
	p := newStub(t)
	reset := time.Date(2026, 1, 1, 18, 40, 0, 0, time.UTC)
	sessionWithPrompt(p, func(context.Context, []acp.ContentBlock, Output) (TurnResult, error) {
		return TurnResult{}, protocol.NewError(protocol.ErrRateLimited, "five-hour window exhausted",
			map[string]any{"resetsAt": reset, "windowId": "five_hour", "retryable": true})
	})
	h := readyHarness(t, p)
	_, err := h.prompt(h.newSession(), "x")
	var re *protocol.RPCError
	if !errors.As(err, &re) {
		t.Fatal(err)
	}
	d, _ := re.Arvel()
	if d.Code != protocol.ErrRateLimited || d.WindowID != "five_hour" || !d.ResetsAt.Equal(reset) {
		t.Fatalf("data = %+v", d)
	}
}

func TestPrompt_StopDetailAndUsageInMeta(t *testing.T) {
	p := newStub(t)
	reset := time.Date(2026, 1, 1, 18, 40, 0, 0, time.UTC)
	sessionWithPrompt(p, func(context.Context, []acp.ContentBlock, Output) (TurnResult, error) {
		return TurnResult{StopReason: acp.StopReasonRefusal, Stop: &protocol.StopDetail{
			Code: protocol.ErrRateLimited, ResetsAt: reset, WindowID: "five_hour", Retryable: true}}, nil
	})
	h := readyHarness(t, p)
	res, err := h.prompt(h.newSession(), "x")
	if err != nil {
		t.Fatal(err)
	}
	got, ok, merr := protocol.MetaFrom[protocol.TurnMeta](mustMeta(t, res.Meta))
	if merr != nil || !ok || got.Stop == nil || got.Stop.Code != protocol.ErrRateLimited {
		t.Fatalf("meta = %+v %v %v", got, ok, merr)
	}
}

func mustMeta(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPrompt_PanicKeepsSessionUsable(t *testing.T) {
	p := newStub(t)
	var runs atomic.Int32
	sessionWithPrompt(p, func(context.Context, []acp.ContentBlock, Output) (TurnResult, error) {
		if runs.Add(1) == 1 {
			panic("prompt boom " + testSecret)
		}
		return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
	})
	h := readyHarness(t, p)
	id := h.newSession()
	if _, err := h.prompt(id, "x"); arvelCode(err) != protocol.ErrInternal {
		t.Fatalf("panicking prompt: %v", err)
	}
	if _, err := h.prompt(id, "y"); err != nil {
		t.Fatalf("the session must stay usable: %v", err)
	}
	if len(h.stderr.find(t, "provider panic")) != 1 {
		t.Fatalf("panic not logged once:\n%s", h.stderr.String())
	}
}

func TestPrompt_TwoSessionsInterleave(t *testing.T) {
	p := newStub(t)
	var started sync.WaitGroup
	started.Add(2)
	sessionWithPrompt(p, func(ctx context.Context, _ []acp.ContentBlock, _ Output) (TurnResult, error) {
		started.Done()
		done := make(chan struct{})
		go func() { started.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			return TurnResult{}, errors.New("the other session never started")
		}
		return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
	})
	h := readyHarness(t, p)
	a, b := h.newSession(), h.newSession()
	ca, cb := h.promptAsync(a, "a"), h.promptAsync(b, "b")
	for _, c := range []<-chan promptOutcome{ca, cb} {
		if out := recv(t, c, "prompt"); out.err != nil {
			t.Fatal(out.err)
		}
	}
}

func TestCancel_EndsTurnWithCancelled(t *testing.T) {
	p := newStub(t)
	cancelled := make(chan struct{})
	var once sync.Once
	started := make(chan struct{}, 1)
	var runs atomic.Int32
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		s := p.makeSession("")
		s.onCancel = func() { once.Do(func() { close(cancelled) }) }
		s.prompt = func(ctx context.Context, _ []acp.ContentBlock, _ Output) (TurnResult, error) {
			if runs.Add(1) > 1 {
				return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
			}
			started <- struct{}{}
			<-cancelled
			return TurnResult{}, context.Canceled
		}
		return s, nil
	}
	h := readyHarness(t, p)
	id := h.newSession()
	out := h.promptAsync(id, "x")
	recv(t, started, "turn start")
	h.cancelSession(id)
	res := recv(t, out, "prompt result")
	if res.err != nil || res.res.StopReason != "cancelled" {
		t.Fatalf("result = %+v", res)
	}
	if p.session(0).cancelN.Load() != 1 {
		t.Fatalf("Session.Cancel called %d times", p.session(0).cancelN.Load())
	}
	h.cancelSession(id) // no turn: a no-op
	h.cancelSession("unknown")
	// A request after both proves the notifications were handled and did no harm.
	if _, err := h.prompt(id, "again"); err != nil {
		t.Fatal(err)
	}
	if p.session(0).cancelN.Load() != 1 {
		t.Fatalf("a cancel without a turn reached the provider: %d calls", p.session(0).cancelN.Load())
	}
}

func TestCancel_WatchdogWarnsAfter10s(t *testing.T) {
	p := newStub(t)
	wait, release := gate()
	started := make(chan struct{}, 1)
	sessionWithPrompt(p, func(context.Context, []acp.ContentBlock, Output) (TurnResult, error) {
		started <- struct{}{}
		wait() // ignores the cancel
		return TurnResult{StopReason: acp.StopReasonCancelled}, nil
	})
	h := readyHarness(t, p)
	t.Cleanup(release)
	id := h.newSession()
	base := h.clock.Pending() // the idle reaper's timer
	out := h.promptAsync(id, "x")
	recv(t, started, "turn start")
	h.cancelSession(id)
	waitFor(t, func() bool { return h.clock.Pending() == base+1 })

	h.clock.Advance(9 * time.Second)
	if n := len(h.stderr.find(t, "prompt still running after cancel")); n != 0 {
		t.Fatalf("warned after 9 s (%d)", n)
	}
	h.clock.Advance(time.Second)
	warns := h.stderr.find(t, "prompt still running after cancel")
	if len(warns) != 1 || warns[0]["session"] != id {
		t.Fatalf("warnings = %v", warns)
	}
	release()
	if res := recv(t, out, "prompt result"); res.err != nil {
		t.Fatal(res.err)
	}
	if h.clock.Pending() != base {
		t.Fatalf("watchdog timer left running: %d pending, want %d", h.clock.Pending(), base)
	}
}

func TestShutdown_CancelsRunningTurn(t *testing.T) {
	p := newStub(t)
	started := make(chan struct{}, 1)
	sawCtxDone := make(chan struct{})
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		s := p.makeSession("")
		s.prompt = func(ctx context.Context, _ []acp.ContentBlock, _ Output) (TurnResult, error) {
			started <- struct{}{}
			<-ctx.Done()
			close(sawCtxDone)
			return TurnResult{}, ctx.Err()
		}
		return s, nil
	}
	h := readyHarness(t, p)
	id := h.newSession()
	h.promptAsync(id, "x")
	recv(t, started, "turn start")
	h.shutdown()
	select {
	case <-sawCtxDone:
	default:
		t.Fatal("the turn context was not cancelled at shutdown")
	}
	if p.session(0).cancelN.Load() != 1 || p.session(0).closeN.Load() != 1 || h.runErr != nil {
		t.Fatalf("cancel %d, close %d, Run %v", p.session(0).cancelN.Load(), p.session(0).closeN.Load(), h.runErr)
	}
}

func TestShutdown_StuckPromptDoesNotBlockExit(t *testing.T) {
	p := newStub(t)
	wait, release := gate()
	started := make(chan struct{}, 1)
	sessionWithPrompt(p, func(context.Context, []acp.ContentBlock, Output) (TurnResult, error) {
		started <- struct{}{}
		wait() // ignores the cancel and the context
		return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
	})
	h := readyHarness(t, p, WithCloseTimeout(100*time.Millisecond))
	t.Cleanup(release) // runs before the harness cleanup
	id := h.newSession()
	h.promptAsync(id, "x")
	recv(t, started, "turn start")
	begin := time.Now()
	h.shutdown()
	if time.Since(begin) > 3*time.Second || h.runErr != nil {
		t.Fatalf("shutdown took %v, Run = %v", time.Since(begin), h.runErr)
	}
	warns := h.stderr.find(t, "prompts still running at shutdown")
	if len(warns) != 1 {
		t.Fatalf("missing warning:\n%s", h.stderr.String())
	}
}
