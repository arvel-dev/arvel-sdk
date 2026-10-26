// SPDX-License-Identifier: Apache-2.0

package spike

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// Probe 6 (SPEC §5.3 point 6): the client side of the library can drive an
// agent through a whole exchange with its public API only. This is what
// testkit/mockhost and the Arvel host need.
func TestProbe6_ClientSideDrivesAgent(t *testing.T) {
	p := newPair(t)
	p.agentImpl.setExt("_arvel/status", func(context.Context, json.RawMessage) (any, error) {
		return map[string]any{"state": "ready"}, nil
	})
	p.agentImpl.promptFn = func(ctx context.Context, req acp.PromptRequest) (acp.PromptResponse, error) {
		for i := 0; i < 50; i++ {
			n := acp.SessionNotification{SessionId: req.SessionId, Update: acp.UpdateAgentMessageText(fmt.Sprintf("chunk-%d", i))}
			if err := p.agent.SessionUpdate(ctx, n); err != nil {
				return acp.PromptResponse{}, err
			}
		}
		tool := acp.SessionNotification{SessionId: req.SessionId, Update: acp.StartToolCall("t_1", "Run tests")}
		if err := p.agent.SessionUpdate(ctx, tool); err != nil {
			return acp.PromptResponse{}, err
		}
		if _, err := p.agent.RequestPermission(ctx, permissionRequest(req.SessionId)); err != nil {
			return acp.PromptResponse{}, err
		}
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	steps := map[string]bool{}
	step := func(name string, ok bool, detail string) {
		steps[name] = ok
		t.Logf("step %-22s ok=%v %s", name, ok, detail)
	}

	initResp, err := p.client.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersion(acp.ProtocolVersionNumber),
		ClientInfo:      &acp.Implementation{Name: "arvel", Version: "1.0.0"},
		Meta:            map[string]any{"arvel": map[string]any{"pluginApi": "1"}},
	})
	step("initialize", err == nil, fmt.Sprintf("err=%v protocolVersion=%v", err, initResp.ProtocolVersion))

	sess, err := p.client.NewSession(ctx, acp.NewSessionRequest{Cwd: "/tmp", McpServers: []acp.McpServer{}})
	step("session/new", err == nil, fmt.Sprintf("err=%v id=%s", err, sess.SessionId))

	resp, err := p.client.Prompt(ctx, textPrompt(sess.SessionId))
	step("session/prompt", err == nil && resp.StopReason == acp.StopReasonEndTurn, fmt.Sprintf("err=%v stop=%s", err, resp.StopReason))

	// Notifications of one session must arrive in order, and all of them
	// before the prompt response.
	got := len(p.clientImpl.updates)
	ordered := got == 51
	for i := 0; ordered && i < 50; i++ {
		n := <-p.clientImpl.updates
		chunk := n.Update.AgentMessageChunk
		if chunk == nil || chunk.Content.Text == nil || chunk.Content.Text.Text != fmt.Sprintf("chunk-%d", i) {
			ordered = false
		}
	}
	step("updates in order", ordered, fmt.Sprintf("%d of 51 updates were queued when the prompt returned", got))
	step("permission delivered", len(p.clientImpl.permReqs) == 1, fmt.Sprintf("%d request(s)", len(p.clientImpl.permReqs)))

	raw, err := p.client.CallExtension(ctx, "_arvel/status", map[string]any{})
	step("_arvel/status", err == nil, fmt.Sprintf("err=%v result=%s", err, truncate(raw, 128)))

	err = p.client.NotifyExtension(ctx, "_arvel/shutdown", map[string]any{})
	shutdownSeen := false
	for err == nil && !shutdownSeen {
		select {
		case c := <-p.agentImpl.extNotify:
			shutdownSeen = c.Method == "_arvel/shutdown"
		case <-time.After(2 * time.Second):
			err = context.DeadlineExceeded
		}
	}
	step("_arvel/shutdown", err == nil && shutdownSeen, fmt.Sprintf("err=%v", err))

	all := true
	for _, ok := range steps {
		all = all && ok
	}
	logProbe(t, 6, all && len(steps) == 7, fmt.Sprintf("%d steps ok of 7, public API only", countTrue(steps)))
}

func countTrue(m map[string]bool) int {
	n := 0
	for _, ok := range m {
		if ok {
			n++
		}
	}
	return n
}

// Observation (not a SPEC point): what does the library put on the wire when
// the context of a pending call is cancelled? SPEC §5.3 wants nothing.
func TestObservation_ContextCancelSendsCancelMessage(t *testing.T) {
	p := newPair(t)

	started := make(chan struct{})
	agentCtxDone := make(chan bool, 1)
	p.agentImpl.promptFn = func(ctx context.Context, _ acp.PromptRequest) (acp.PromptResponse, error) {
		close(started)
		select {
		case <-ctx.Done():
			agentCtxDone <- true
		case <-time.After(2 * time.Second):
			agentCtxDone <- false
		}
		return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := p.client.Prompt(ctx, textPrompt("s_1"))
		done <- err
	}()

	<-started
	before := p.clientToAgent.Len()
	cancel()
	select {
	case err := <-done:
		t.Logf("client Prompt returned after cancel: err=%v", err)
	case <-time.After(5 * time.Second):
		t.Log("client Prompt did not return within 5s after cancel")
	}
	time.Sleep(300 * time.Millisecond)

	extra := p.clientToAgent.From(before)
	if len(extra) == 0 {
		t.Log("OBSERVATION cancel-on-context: nothing was sent on the wire")
	} else {
		t.Logf("OBSERVATION cancel-on-context: %d bytes sent after cancel: %s", len(extra), truncate(extra, 512))
	}
	select {
	case seen := <-agentCtxDone:
		t.Logf("OBSERVATION cancel-on-context: agent handler context cancelled=%v", seen)
	case <-time.After(3 * time.Second):
		t.Log("OBSERVATION cancel-on-context: agent handler never finished")
	}
}
