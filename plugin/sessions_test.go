// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

const testCWD = "/tmp/work"

// sessionID creates a session with minimal params and returns its id.
func (h *harness) newSession() string {
	h.t.Helper()
	var res struct {
		SessionID string `json:"sessionId"`
	}
	err := h.call(protocol.MethodSessionNew, map[string]any{"cwd": testCWD, "mcpServers": []any{}}, &res)
	if err != nil {
		h.t.Fatalf("session/new: %v", err)
	}
	return res.SessionID
}

type promptResult struct {
	StopReason string          `json:"stopReason"`
	Meta       json.RawMessage `json:"_meta"`
}

func (h *harness) prompt(id, text string) (promptResult, error) {
	h.t.Helper()
	var res promptResult
	err := h.call(protocol.MethodSessionPrompt, map[string]any{
		"sessionId": id,
		"prompt":    []any{map[string]any{"type": "text", "text": text}},
	}, &res)
	return res, err
}

// promptAsync runs a prompt in the background.
func (h *harness) promptAsync(id, text string) <-chan promptOutcome {
	ch := make(chan promptOutcome, 1)
	go func() {
		res, err := h.prompt(id, text)
		ch <- promptOutcome{res, err}
	}()
	return ch
}

type promptOutcome struct {
	res promptResult
	err error
}

func (h *harness) cancelSession(id string) {
	h.t.Helper()
	if err := h.client.Notify(protocol.MethodSessionCancel, map[string]any{"sessionId": id}); err != nil {
		h.t.Fatal(err)
	}
}

func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %s", what)
		var zero T
		return zero
	}
}

func withFeatures(t *testing.T, f Features) *stubProvider {
	p := newStub(t)
	p.feats = f
	return p
}

func TestSessionNew_Golden(t *testing.T) {
	p := newStub(t)
	h := readyHarness(t, p)
	var res struct {
		SessionID string `json:"sessionId"`
	}
	if err := h.call(protocol.MethodSessionNew, golden(t, "7.1-session-new.json", "/params"), &res); err != nil {
		t.Fatal(err)
	}
	if res.SessionID != "s-1" {
		t.Fatalf("session id = %q", res.SessionID)
	}
	want := goldenAs[protocol.SessionMeta](t, "7.1-session-new.json", "/params/_meta/arvel")
	if p.lastNew.CWD != "/srv/arvel/worktrees/shop/task-42" || !reflect.DeepEqual(p.lastNew.Meta, want) {
		t.Fatalf("provider saw %+v", p.lastNew)
	}
	if len(p.lastNew.MCPServers) != 1 {
		t.Fatalf("mcp servers = %+v", p.lastNew.MCPServers)
	}
}

func TestSessionNew_ConfigOptionsFromSession(t *testing.T) {
	p := newStub(t)
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		s := p.makeSession("")
		s.options = []acp.SessionConfigOption{{Boolean: &acp.SessionConfigOptionBoolean{
			Id: "thinking", Name: "Thinking", Type: "boolean", CurrentValue: true}}}
		return stubSessionWithOptions{s}, nil
	}
	h := readyHarness(t, p)
	var res struct {
		ConfigOptions []map[string]any `json:"configOptions"`
	}
	if err := h.call(protocol.MethodSessionNew, map[string]any{"cwd": testCWD, "mcpServers": []any{}}, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.ConfigOptions) != 1 || res.ConfigOptions[0]["id"] != "thinking" {
		t.Fatalf("config options = %+v", res.ConfigOptions)
	}
}

func TestSessionNew_Errors(t *testing.T) {
	newReq := func(m map[string]any) map[string]any { return m }
	tests := []struct {
		name   string
		params map[string]any
		setup  func(p *stubProvider)
		code   int
		arvel  protocol.ErrorCode
	}{
		{name: "missing_cwd", params: newReq(map[string]any{"mcpServers": []any{}}), code: protocol.CodeInvalidParams},
		{name: "missing_mcp_servers", params: newReq(map[string]any{"cwd": testCWD}), code: protocol.CodeInvalidParams},
		{name: "invalid_permission_level", params: newReq(map[string]any{"cwd": testCWD, "mcpServers": []any{},
			"_meta": map[string]any{"arvel": map[string]any{"permissions": map[string]any{"read": "maybe"}}}}), code: protocol.CodeInvalidParams},
		{name: "provider_error", params: newReq(map[string]any{"cwd": testCWD, "mcpServers": []any{}}),
			setup: func(p *stubProvider) {
				p.newSession = func(context.Context, SessionParams) (Session, error) {
					return nil, protocol.NewError(protocol.ErrAuthRequired, "sign in", nil)
				}
			}, arvel: protocol.ErrAuthRequired},
		{name: "nil_session", params: newReq(map[string]any{"cwd": testCWD, "mcpServers": []any{}}),
			setup: func(p *stubProvider) {
				p.newSession = func(context.Context, SessionParams) (Session, error) { return nil, nil }
			}, arvel: protocol.ErrInternal},
		{name: "empty_session_id", params: newReq(map[string]any{"cwd": testCWD, "mcpServers": []any{}}),
			setup: func(p *stubProvider) {
				p.newSession = func(context.Context, SessionParams) (Session, error) { return &stubSession{}, nil }
			}, arvel: protocol.ErrInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newStub(t)
			if tc.setup != nil {
				tc.setup(p)
			}
			h := readyHarness(t, p)
			err := h.call(protocol.MethodSessionNew, tc.params, nil)
			if tc.code != 0 && rpcCode(err) != tc.code {
				t.Fatalf("err = %v, want code %d", err, tc.code)
			}
			if tc.arvel != "" && arvelCode(err) != tc.arvel {
				t.Fatalf("err = %v, want arvel %s", err, tc.arvel)
			}
		})
	}
}

func TestSessionNew_DuplicateIDClosesSecond(t *testing.T) {
	p := newStub(t)
	first := &stubSession{id: "same"}
	second := &stubSession{id: "same"}
	calls := 0
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		calls++
		if calls == 1 {
			return first, nil
		}
		return second, nil
	}
	h := readyHarness(t, p)
	h.newSession()
	err := h.call(protocol.MethodSessionNew, map[string]any{"cwd": testCWD, "mcpServers": []any{}}, nil)
	if arvelCode(err) != protocol.ErrInternal || second.closeN.Load() != 1 || first.closeN.Load() != 0 {
		t.Fatalf("err = %v, second closes = %d, first closes = %d", err, second.closeN.Load(), first.closeN.Load())
	}
}

func TestSessionLoad_NotAdvertisedIsMethodNotFound(t *testing.T) {
	h := readyHarness(t, newStub(t))
	err := h.call(protocol.MethodSessionLoad, map[string]any{"sessionId": "x", "cwd": testCWD, "mcpServers": []any{}}, nil)
	if rpcCode(err) != protocol.CodeMethodNotFound {
		t.Fatalf("err = %v, want -32601", err)
	}
}

func TestSessionLoad_ResumesAndIsIdempotent(t *testing.T) {
	p := withFeatures(t, Features{LoadSession: true})
	h := readyHarness(t, p)
	params := map[string]any{"sessionId": "old-1", "cwd": testCWD, "mcpServers": []any{},
		"_meta": map[string]any{"arvel": map[string]any{"model": "m1"}}}
	if err := h.call(protocol.MethodSessionLoad, params, nil); err != nil {
		t.Fatal(err)
	}
	if p.loadN.Load() != 1 || p.lastLoad.SessionID != "old-1" || p.lastLoad.CWD != testCWD || p.lastLoad.Meta.Model != "m1" {
		t.Fatalf("provider saw %+v", p.lastLoad)
	}
	if err := h.call(protocol.MethodSessionLoad, params, nil); err != nil || p.loadN.Load() != 1 {
		t.Fatalf("second load: %v, provider calls = %d", err, p.loadN.Load())
	}
	if _, err := h.prompt("old-1", "hi"); err != nil {
		t.Fatalf("a loaded session must accept prompts: %v", err)
	}
}

func TestSessionLoad_WrongIDFromProvider(t *testing.T) {
	p := withFeatures(t, Features{LoadSession: true})
	other := &stubSession{id: "other"}
	p.loadSession = func(context.Context, LoadParams) (Session, error) { return other, nil }
	h := readyHarness(t, p)
	err := h.call(protocol.MethodSessionLoad, map[string]any{"sessionId": "wanted", "cwd": testCWD, "mcpServers": []any{}}, nil)
	if arvelCode(err) != protocol.ErrInternal || other.closeN.Load() != 1 {
		t.Fatalf("err = %v, closes = %d", err, other.closeN.Load())
	}
}

func TestSetConfigOption(t *testing.T) {
	p := newStub(t)
	var gotID, gotValue string
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		s := p.makeSession("")
		s.setOption = func(_ context.Context, id, value string) ([]acp.SessionConfigOption, error) {
			gotID, gotValue = id, value
			return nil, nil // nil must become an empty array on the wire
		}
		return s, nil
	}
	h := readyHarness(t, p)
	id := h.newSession()

	var raw json.RawMessage
	err := h.call(protocol.MethodSessionSetConfigOption, map[string]any{"sessionId": id, "configId": "model", "value": "opus"}, &raw)
	if err != nil {
		t.Fatal(err)
	}
	if gotID != "model" || gotValue != "opus" {
		t.Fatalf("provider saw %q=%q", gotID, gotValue)
	}
	if !strings.Contains(string(raw), `"configOptions":[]`) {
		t.Fatalf("response = %s", raw)
	}
	err = h.call(protocol.MethodSessionSetConfigOption, map[string]any{"sessionId": "nope", "configId": "model", "value": "x"}, nil)
	if rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("unknown session: %v", err)
	}
	err = h.call(protocol.MethodSessionSetConfigOption, map[string]any{"sessionId": id, "type": "boolean", "configId": "thinking", "value": true}, nil)
	if rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("boolean option: %v", err)
	}
}

func TestShutdown_ClosesAllSessionsInParallel(t *testing.T) {
	p := newStub(t)
	var arrived atomic.Int32
	var allMet atomic.Bool
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		s := p.makeSession("")
		s.onClose = func(ctx context.Context) error {
			arrived.Add(1)
			for arrived.Load() < 3 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Millisecond):
				}
			}
			allMet.Store(true)
			return nil
		}
		return s, nil
	}
	h := readyHarness(t, p)
	h.newSession()
	h.newSession()
	h.newSession()
	h.shutdown()
	if !allMet.Load() {
		t.Fatal("sessions were closed one after another, not in parallel")
	}
	for i := range 3 {
		if p.session(i).closeN.Load() != 1 {
			t.Fatalf("session %d closed %d times", i, p.session(i).closeN.Load())
		}
	}
	if h.runErr != nil {
		t.Fatalf("Run = %v", h.runErr)
	}
}

func TestShutdown_CloseTimeout(t *testing.T) {
	p := newStub(t)
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		s := p.makeSession("")
		s.onClose = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
		return s, nil
	}
	h := readyHarness(t, p, WithCloseTimeout(50*time.Millisecond))
	id := h.newSession()
	h.shutdown()
	// The failing Close and the deadline race, so the line may come a moment later.
	waitFor(t, func() bool { return len(h.stderr.find(t, "session close failed")) == 1 })
	if logs := h.stderr.find(t, "session close failed"); logs[0]["session"] != id {
		t.Fatalf("close failure logged for %v, want %s", logs[0]["session"], id)
	}
}

func TestShutdown_PanickingCloseDoesNotStopOthers(t *testing.T) {
	p := newStub(t)
	var mu sync.Mutex
	n := 0
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		s := p.makeSession("")
		mu.Lock()
		n++
		first := n == 1
		mu.Unlock()
		if first {
			s.onClose = func(context.Context) error { panic("close boom") }
		}
		return s, nil
	}
	h := readyHarness(t, p)
	h.newSession()
	h.newSession()
	h.shutdown()
	if p.session(1).closeN.Load() != 1 || h.runErr != nil {
		t.Fatalf("second session closes = %d, Run = %v", p.session(1).closeN.Load(), h.runErr)
	}
}
