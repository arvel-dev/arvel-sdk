// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/logx"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// e2eProvider returns a provider whose first session streams and asks for a
// permission, and whose second session blocks on a permission request.
func e2eProvider(t *testing.T) *stubProvider {
	p := withFeatures(t, goldenFeatures(t))
	turnMeta := goldenAs[protocol.TurnMeta](t, "7.2-turn-meta.json", "/_meta/arvel")
	var mu sync.Mutex
	n := 0
	p.newSession = func(context.Context, SessionParams) (Session, error) {
		s := p.makeSession("")
		mu.Lock()
		n++
		first := n == 1
		mu.Unlock()
		s.prompt = func(ctx context.Context, _ []acp.ContentBlock, out Output) (TurnResult, error) {
			_ = out.Update(acp.UpdateAgentMessageText("hello "), nil)
			_ = out.Update(acp.UpdateAgentMessageText("world"), &protocol.UpdateMeta{
				Subagent: &protocol.SubagentMeta{ID: "sub", ParentToolCallID: "t_9", Label: "Explore"}})
			d, err := out.RequestPermission(ctx, permissionRequest(t))
			if err != nil {
				return TurnResult{}, err
			}
			if d.Cancelled {
				return TurnResult{StopReason: acp.StopReasonCancelled}, nil
			}
			_ = out.Update(acp.UpdateAgentMessageText("done"), nil)
			if first {
				return TurnResult{StopReason: acp.StopReasonEndTurn, Usage: turnMeta.Usage}, nil
			}
			return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
		}
		return s, nil
	}
	return p
}

func TestE2E_ProviderConversation(t *testing.T) {
	p := e2eProvider(t)
	h := newHarness(t, p)
	log := h.collectUpdates()

	// initialize (golden 5.1) -> status, models, usage, action (6.x)
	var res json.RawMessage
	if err := h.call("initialize", golden(t, "5.1-initialize-request.json", "/params"), &res); err != nil {
		t.Fatal(err)
	}
	var st json.RawMessage
	if err := h.call(protocol.MethodStatus, nil, &st); err != nil {
		t.Fatal(err)
	}
	mustEqualJSON(t, golden(t, "6.1-status-result.json", ""), st)

	// session/new (golden 7.1)
	var created struct {
		SessionID string `json:"sessionId"`
	}
	if err := h.call(protocol.MethodSessionNew, golden(t, "7.1-session-new.json", "/params"), &created); err != nil {
		t.Fatal(err)
	}
	a := created.SessionID
	b := h.newSession()

	// prompt with a permission request (golden 7.3) and the turn meta (7.2)
	answered := make(chan struct{}, 4)
	pc := h.handlePermissions(func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(params, &req)
		if req.SessionID == b {
			<-ctx.Done() // the second session's request stays pending
			return nil, ctx.Err()
		}
		answered <- struct{}{}
		return selected("allow_once"), nil
	})
	out, err := h.prompt(a, "go")
	if err != nil || out.StopReason != "end_turn" {
		t.Fatalf("prompt: %+v %v", out, err)
	}
	if got := log.texts(); len(got) != 3 || got[0] != "hello " || got[1] != "world" || got[2] != "done" {
		t.Fatalf("updates = %q", got)
	}
	mustEqualJSON(t, golden(t, "7.2-turn-meta.json", "/_meta"), out.Meta)

	// busy: the second session waits for the host's answer
	blocked := h.promptAsync(b, "wait")
	waitFor(t, func() bool { return pc.count() == 2 })
	if _, err := h.prompt(b, "again"); arvelCode(err) != protocol.ErrBusy || rpcCode(err) != protocol.CodeBusy {
		t.Fatalf("second prompt while a turn runs: %v", err)
	}

	// set_config_option, then cancel with the permission request pending
	if err := h.call(protocol.MethodSessionSetConfigOption, map[string]any{"sessionId": a, "configId": "model", "value": "x"}, nil); err != nil {
		t.Fatal(err)
	}
	h.cancelSession(b)
	if res := recv(t, blocked, "cancelled prompt"); res.err != nil || res.res.StopReason != "cancelled" {
		t.Fatalf("cancelled prompt: %+v", res)
	}

	// push messages
	var mu sync.Mutex
	var pushed []string
	for _, m := range []string{protocol.MethodStatusChanged, protocol.MethodUsageUpdated} {
		h.client.HandleNotification(m, func(_ context.Context, _ json.RawMessage) {
			mu.Lock()
			pushed = append(pushed, m)
			mu.Unlock()
		})
	}
	p.getHost().StatusChanged(protocol.Status{State: protocol.StateNeedsLogin, Message: "signed out"})
	p.getHost().UsageUpdated(goldenAs[protocol.Usage](t, "6.3-usage.json", ""))
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(pushed) == 2 })

	// unknown method
	if err := h.call("_arvel/unknown", nil, nil); rpcCode(err) != protocol.CodeMethodNotFound {
		t.Fatalf("unknown method: %v", err)
	}

	// shutdown
	h.shutdown()
	if h.runErr != nil || p.shutdownN.Load() != 1 {
		t.Fatalf("Run = %v, shutdown calls = %d", h.runErr, p.shutdownN.Load())
	}
	for i := range p.sessionCount() {
		if p.session(i).closeN.Load() != 1 {
			t.Fatalf("session %d closed %d times", i, p.session(i).closeN.Load())
		}
	}
	for _, line := range h.stderr.Lines(t) {
		for _, key := range []string{"ts", "level", "msg", "plugin", "pid"} {
			if _, ok := line[key]; !ok {
				t.Fatalf("log line lacks %q: %v", key, line)
			}
		}
	}
}

func TestE2E_StdoutIsProtocolOnly(t *testing.T) {
	p := e2eProvider(t)
	h := readyHarness(t, p)
	h.handlePermissions(func(context.Context, json.RawMessage) (any, error) { return selected("allow_once"), nil })
	h.collectUpdates()
	if _, err := h.prompt(h.newSession(), "x"); err != nil {
		t.Fatal(err)
	}
	h.shutdown()
	lines := strings.Split(strings.TrimSpace(h.wire.String()), "\n")
	if len(lines) < 5 {
		t.Fatalf("only %d lines on stdout", len(lines))
	}
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("stdout line is not JSON: %q", l)
		}
		_, hasMethod := m["method"]
		_, hasID := m["id"]
		if m["jsonrpc"] != "2.0" || !(hasMethod || hasID) {
			t.Fatalf("stdout line is not a JSON-RPC message: %q", l)
		}
	}
}

const canary = "CANARY-0123456789abcdef"

func TestE2E_CanarySecretNeverLeaks(t *testing.T) {
	t.Setenv(logx.EnvLevel, "debug")
	p := withFeatures(t, Features{})
	p.status = func(context.Context) (protocol.Status, error) {
		return protocol.Status{State: protocol.StateReady, Message: "key " + canary, BillingNote: canary}, nil
	}
	p.models = func(context.Context, bool) (protocol.ModelsResult, error) {
		return protocol.ModelsResult{}, errors.New("vendor rejected " + canary)
	}
	p.usage = func(context.Context) (protocol.Usage, error) {
		return protocol.Usage{}, &protocol.RPCError{Code: -32000, Message: "m " + canary,
			Data: json.RawMessage(`{"arvel":{"code":"provider_error","message":"x ` + canary + `","vendorCode":"v"}}`)}
	}
	p.action = func(context.Context, string) (protocol.ActionResult, error) {
		return protocol.ActionResult{}, fmt.Errorf("wrapped: %w", errors.New(canary))
	}
	sessionWithPrompt(p, func(ctx context.Context, _ []acp.ContentBlock, _ Output) (TurnResult, error) {
		h := p.getHost()
		lg := h.Logger()
		lg.Debug("debug " + canary)
		lg.Info("info", "attr", canary)
		lg.Warn(fmt.Sprintf("warn %s", canary))
		lg.Error("error", "err", errors.New(canary))
		panic("prompt boom " + canary)
	})
	d := stubDiscoverer{stubProvider: p, discover: func(context.Context, protocol.DiscoverParams) (protocol.DiscoverResult, error) {
		return protocol.DiscoverResult{McpServers: []protocol.DiscoveredMcp{{
			Name: "m", Scope: protocol.DiscoverScopeUser, Transport: protocol.McpTransportStdio,
			Command: []string{"srv", "--token=" + canary}}}}, nil
	}}
	h := newHarness(t, d)
	h.mustInitialize(protocol.InitMeta{Connection: protocol.ConnectionInfo{ID: "c1", Kind: "api_key",
		Secrets: map[string]string{"apiKey": canary}}})

	_ = h.call(protocol.MethodStatus, nil, nil)
	_ = h.call(protocol.MethodModels, nil, nil)
	_ = h.call(protocol.MethodUsage, nil, nil)
	_ = h.call(protocol.MethodAction, protocol.ActionParams{ID: "x"}, nil)
	_ = h.call(protocol.MethodDiscover, protocol.DiscoverParams{}, nil)
	_, _ = h.prompt(h.newSession(), "x")
	h.shutdown()

	for name, text := range map[string]string{"stdout": h.wire.String(), "stderr": h.stderr.String()} {
		if strings.Contains(text, canary) {
			t.Errorf("the canary secret leaked to %s:\n%s", name, text)
		}
		if !strings.Contains(text, "‹redacted›") {
			t.Errorf("%s holds no redaction marker; the test did not exercise the paths", name)
		}
	}
}

func TestE2E_LargeLines(t *testing.T) {
	if testing.Short() {
		t.Skip("moves 24 MiB lines")
	}
	const size = 24 << 20 // above the 10 MiB limit of the ACP library (FINDINGS S0.1)
	big := strings.Repeat("x", size)
	want := sha256.Sum256([]byte(big))

	p := newStub(t)
	gotSum := make(chan [32]byte, 1)
	sessionWithPrompt(p, func(_ context.Context, blocks []acp.ContentBlock, out Output) (TurnResult, error) {
		gotSum <- sha256.Sum256([]byte(blocks[0].Text.Text))
		if err := out.Update(acp.UpdateAgentMessageText(big), nil); err != nil {
			return TurnResult{}, err
		}
		return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
	})
	h := readyHarness(t, p)
	var updateLen int
	var mu sync.Mutex
	h.client.HandleNotification(protocol.MethodSessionUpdate, func(_ context.Context, raw json.RawMessage) {
		mu.Lock()
		updateLen = len(raw)
		mu.Unlock()
	})
	id := h.newSession()
	if _, err := h.prompt(id, big); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, gotSum, "prompt text"); got != want {
		t.Fatal("the 24 MiB prompt arrived changed")
	}
	mu.Lock()
	defer mu.Unlock()
	if updateLen < size {
		t.Fatalf("the update arrived with %d bytes, want at least %d", updateLen, size)
	}
}

func TestE2E_OverLimitLineSurvives(t *testing.T) {
	h := readyHarness(t, newStub(t), withMaxLine(1024))
	long := `{"jsonrpc":"2.0","id":99,"method":"_arvel/status","params":"` + strings.Repeat("x", 4096) + `"}` + "\n"
	if _, err := io.WriteString(h.stdin, long); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return strings.Contains(h.wire.String(), `"id":null`) })
	if !strings.Contains(h.wire.String(), `"code":-32600`) {
		t.Fatalf("no -32600 reply:\n%s", h.wire.String())
	}
	if err := h.call(protocol.MethodStatus, nil, nil); err != nil {
		t.Fatalf("the connection must survive an over-long line: %v", err)
	}
}

func TestE2E_NoGoroutineLeakAcrossShutdownPaths(t *testing.T) {
	triggers := map[string]func(h *harness){
		"notification": func(h *harness) { _ = h.client.Notify(protocol.MethodShutdown, nil) },
		"stdin_eof":    func(h *harness) { h.closeStdin() },
		"context":      func(h *harness) { h.cancel() },
	}
	for name, trigger := range triggers {
		t.Run(name, func(t *testing.T) {
			p := withFeatures(t, Features{LoadSession: true})
			started := make(chan struct{}, 2)
			p.newSession = func(context.Context, SessionParams) (Session, error) {
				s := p.makeSession("")
				s.prompt = func(ctx context.Context, _ []acp.ContentBlock, out Output) (TurnResult, error) {
					started <- struct{}{}
					if s.id == "s-2" { // the second session waits for the host's permission answer
						d, err := out.RequestPermission(ctx, permissionRequest(t))
						if err != nil || d.Cancelled {
							return TurnResult{StopReason: acp.StopReasonCancelled}, nil
						}
					}
					<-ctx.Done()
					return TurnResult{StopReason: acp.StopReasonCancelled}, nil
				}
				return s, nil
			}
			h := readyHarness(t, p)
			h.handlePermissions(func(ctx context.Context, _ json.RawMessage) (any, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			})
			a, b := h.newSession(), h.newSession()
			h.promptAsync(a, "a")
			h.promptAsync(b, "b")
			recv(t, started, "first turn")
			recv(t, started, "second turn")
			trigger(h)
			h.waitDone(5 * time.Second)
			if h.runErr != nil {
				t.Fatalf("Run = %v", h.runErr)
			}
			if h.clock.Pending() != 0 {
				t.Fatalf("%d timers pending after shutdown", h.clock.Pending())
			}
		})
	}
}

func TestE2E_ConcurrentSessionsStress(t *testing.T) {
	const sessions, turns, updatesPerTurn = 8, 5, 10
	p := newStub(t)
	sessionWithPrompt(p, func(ctx context.Context, blocks []acp.ContentBlock, out Output) (TurnResult, error) {
		tag := blocks[0].Text.Text
		rng := rand.New(rand.NewSource(int64(len(tag)) + int64(tag[len(tag)-1])))
		for j := range updatesPerTurn {
			time.Sleep(time.Duration(rng.Intn(300)) * time.Microsecond)
			if err := out.Update(acp.UpdateAgentMessageText(fmt.Sprintf("%s-u%d", tag, j)), nil); err != nil {
				return TurnResult{}, err
			}
		}
		return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
	})
	h := readyHarness(t, p)
	log := h.collectUpdates()
	ids := make([]string, sessions)
	for i := range ids {
		ids[i] = h.newSession()
	}
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range turns {
				tag := fmt.Sprintf("s%d-t%d", i, k)
				if _, err := h.prompt(id, tag); err != nil {
					t.Errorf("%s: %v", tag, err)
					return
				}
				// Every update of the turn must have been handled before the response.
				seen := 0
				for _, u := range log.snapshot() {
					if strings.HasPrefix(u.Update.Content.Text, tag+"-u") {
						if want := fmt.Sprintf("%s-u%d", tag, seen); u.Update.Content.Text != want {
							t.Errorf("%s: update %d is %q, want %q", tag, seen, u.Update.Content.Text, want)
							return
						}
						seen++
					}
				}
				if seen != updatesPerTurn {
					t.Errorf("%s: response arrived after %d of %d updates", tag, seen, updatesPerTurn)
					return
				}
			}
		}()
	}
	wg.Wait()
}
