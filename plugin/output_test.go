// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/jsonrpc"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// permissionRequest builds the request of protocol/testdata/7.3.
func permissionRequest(t *testing.T) PermissionRequest {
	t.Helper()
	g := goldenAs[acp.RequestPermissionRequest](t, "7.3-request-permission.json", "/params")
	return PermissionRequest{
		ToolCall: g.ToolCall,
		Options:  g.Options,
		Meta:     goldenAs[protocol.PermissionMeta](t, "7.3-request-permission.json", "/params/_meta/arvel"),
	}
}

// permissionStub makes sessions whose Prompt asks for one permission and
// reports the decision on decided.
func permissionStub(t *testing.T, decided chan<- PermissionDecision) *stubProvider {
	p := newStub(t)
	sessionWithPrompt(p, func(ctx context.Context, _ []acp.ContentBlock, out Output) (TurnResult, error) {
		d, err := out.RequestPermission(ctx, permissionRequest(t))
		if err != nil {
			return TurnResult{}, err
		}
		decided <- d
		if d.Cancelled {
			return TurnResult{StopReason: acp.StopReasonCancelled}, nil
		}
		return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
	})
	return p
}

type permissionClient struct {
	mu       sync.Mutex
	requests []json.RawMessage
	answer   func(ctx context.Context, params json.RawMessage) (any, error)
}

func (h *harness) handlePermissions(answer func(ctx context.Context, params json.RawMessage) (any, error)) *permissionClient {
	c := &permissionClient{answer: answer}
	h.client.Handle(protocol.MethodSessionRequestPermission, func(ctx context.Context, params json.RawMessage) (any, error) {
		c.mu.Lock()
		c.requests = append(c.requests, params)
		c.mu.Unlock()
		return c.answer(ctx, params)
	})
	return c
}

func selected(id string) any {
	return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(acp.PermissionOptionId(id))}
}

func (c *permissionClient) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

func TestPermission_Roundtrip(t *testing.T) {
	decided := make(chan PermissionDecision, 1)
	h := readyHarness(t, permissionStub(t, decided))
	pc := h.handlePermissions(func(context.Context, json.RawMessage) (any, error) { return selected("allow_once"), nil })
	id := h.newSession()
	if _, err := h.prompt(id, "run it"); err != nil {
		t.Fatal(err)
	}
	if d := recv(t, decided, "decision"); d.OptionID != "allow_once" || d.Cancelled {
		t.Fatalf("decision = %+v", d)
	}
	var want map[string]any
	if err := json.Unmarshal(golden(t, "7.3-request-permission.json", "/params"), &want); err != nil {
		t.Fatal(err)
	}
	want["sessionId"] = id
	mustEqualJSON(t, want, pc.requests[0])
}

func TestPermission_NoTimeout(t *testing.T) {
	decided := make(chan PermissionDecision, 1)
	h := readyHarness(t, permissionStub(t, decided))
	answer := make(chan struct{})
	h.handlePermissions(func(ctx context.Context, _ json.RawMessage) (any, error) {
		select {
		case <-answer:
		case <-ctx.Done():
		}
		return selected("allow_always"), nil
	})
	out := h.promptAsync(h.newSession(), "x")
	time.Sleep(50 * time.Millisecond)
	h.clock.Advance(24 * time.Hour)
	select {
	case d := <-decided:
		t.Fatalf("decided without an answer after 24 h: %+v", d)
	case <-time.After(100 * time.Millisecond):
	}
	close(answer)
	if d := recv(t, decided, "decision"); d.OptionID != "allow_always" {
		t.Fatalf("decision = %+v", d)
	}
	if res := recv(t, out, "prompt result"); res.err != nil {
		t.Fatal(res.err)
	}
}

func TestPermission_CancelledBySessionCancel(t *testing.T) {
	decided := make(chan PermissionDecision, 1)
	p := permissionStub(t, decided)
	h := readyHarness(t, p)
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	pc := h.handlePermissions(func(ctx context.Context, _ json.RawMessage) (any, error) {
		<-hold
		return selected("allow_once"), nil
	})
	id := h.newSession()
	out := h.promptAsync(id, "x")
	waitFor(t, func() bool { return pc.count() == 1 })
	h.cancelSession(id)
	if d := recv(t, decided, "decision"); !d.Cancelled || d.OptionID != "" {
		t.Fatalf("decision = %+v, want cancelled", d)
	}
	res := recv(t, out, "prompt result")
	if res.err != nil || res.res.StopReason != "cancelled" {
		t.Fatalf("result = %+v", res)
	}
}

func TestPermission_BadAnswersAreRejections(t *testing.T) {
	tests := []struct {
		name   string
		answer func(context.Context, json.RawMessage) (any, error)
	}{
		{"unknown_option", func(context.Context, json.RawMessage) (any, error) { return selected("approve_everything"), nil }},
		{"host_error", func(context.Context, json.RawMessage) (any, error) {
			return nil, protocol.NewError(protocol.ErrInternal, "cannot ask", nil)
		}},
		{"outcome_cancelled", func(context.Context, json.RawMessage) (any, error) {
			return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
		}},
		{"empty_outcome", func(context.Context, json.RawMessage) (any, error) { return map[string]any{}, nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decided := make(chan PermissionDecision, 1)
			h := readyHarness(t, permissionStub(t, decided))
			h.handlePermissions(tc.answer)
			if _, err := h.prompt(h.newSession(), "x"); err != nil {
				t.Fatal(err)
			}
			if d := recv(t, decided, "decision"); !d.Cancelled {
				t.Fatalf("decision = %+v, must be a rejection", d)
			}
		})
	}
}

func TestPermission_InvalidRequestNotSent(t *testing.T) {
	errs := make(chan error, 2)
	p := newStub(t)
	sessionWithPrompt(p, func(ctx context.Context, _ []acp.ContentBlock, out Output) (TurnResult, error) {
		bad := permissionRequest(t)
		bad.Meta.Category = "bogus"
		_, err1 := out.RequestPermission(ctx, bad)
		noOpts := permissionRequest(t)
		noOpts.Options = nil
		_, err2 := out.RequestPermission(ctx, noOpts)
		errs <- err1
		errs <- err2
		return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
	})
	h := readyHarness(t, p)
	pc := h.handlePermissions(func(context.Context, json.RawMessage) (any, error) { return selected("allow_once"), nil })
	if _, err := h.prompt(h.newSession(), "x"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := recv(t, errs, "error"); err == nil {
			t.Fatal("an invalid permission request must fail")
		}
	}
	if pc.count() != 0 {
		t.Fatalf("%d invalid requests reached the host", pc.count())
	}
}

func TestPermission_ConcurrentSessionsIndependent(t *testing.T) {
	p := newStub(t)
	var n int
	var mu sync.Mutex
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		s := p.makeSession("")
		mu.Lock()
		n++
		asks := n == 1 // the first session asks and stays pending
		mu.Unlock()
		s.prompt = func(ctx context.Context, _ []acp.ContentBlock, out Output) (TurnResult, error) {
			if asks {
				if _, err := out.RequestPermission(ctx, permissionRequest(t)); err != nil {
					return TurnResult{}, err
				}
			}
			return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
		}
		return s, nil
	}
	h := readyHarness(t, p)
	hold := make(chan struct{})
	pc := h.handlePermissions(func(ctx context.Context, _ json.RawMessage) (any, error) {
		select {
		case <-hold:
		case <-ctx.Done():
		}
		return selected("allow_once"), nil
	})
	a, b := h.newSession(), h.newSession()
	outA := h.promptAsync(a, "a")
	waitFor(t, func() bool { return pc.count() == 1 })
	if _, err := h.prompt(b, "b"); err != nil {
		t.Fatalf("session b must not wait for session a: %v", err)
	}
	close(hold)
	if res := recv(t, outA, "session a"); res.err != nil {
		t.Fatal(res.err)
	}
}

func TestUpdate_AfterPeerClosed(t *testing.T) {
	updateErr := make(chan error, 1)
	p := newStub(t)
	sessionWithPrompt(p, func(ctx context.Context, _ []acp.ContentBlock, out Output) (TurnResult, error) {
		<-ctx.Done() // ends at shutdown, after the connection closed
		updateErr <- out.Update(acp.UpdateAgentMessageText("late"), nil)
		return TurnResult{}, ctx.Err()
	})
	h := readyHarness(t, p)
	id := h.newSession()
	started := h.promptAsync(id, "x")
	time.Sleep(50 * time.Millisecond)
	h.closeStdin()
	h.waitDone(5 * time.Second)
	if err := recv(t, updateErr, "update error"); !errors.Is(err, jsonrpc.ErrClosed) {
		t.Fatalf("Update after close = %v, want jsonrpc.ErrClosed", err)
	}
	<-started
}
