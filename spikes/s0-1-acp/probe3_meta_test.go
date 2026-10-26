// SPDX-License-Identifier: Apache-2.0

package spike

import (
	"context"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// Probe 3 (SPEC §5.3 point 3): _meta survives the wire on initialize,
// session/new, the session/prompt result, session/update and
// session/request_permission.
func TestProbe3_MetaRoundTrip(t *testing.T) {
	p := newPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results := map[string]bool{}
	record := func(name string, ok bool, detail string) {
		results[name] = ok
		logProbe(t, 3, ok, name+": "+detail)
	}

	t.Run("initialize", func(t *testing.T) {
		resp, err := p.client.Initialize(ctx, acp.InitializeRequest{
			ProtocolVersion: acp.ProtocolVersion(acp.ProtocolVersionNumber),
			Meta:            arvelMeta(),
		})
		if err != nil {
			t.Fatalf("initialize: %v", err)
		}
		p.agentImpl.mu.Lock()
		reqOK := arvelMetaOK(p.agentImpl.initMeta)
		p.agentImpl.mu.Unlock()
		respOK := arvelMetaOK(resp.Meta)
		record("initialize", reqOK && respOK, "request meta="+boolStr(reqOK)+", response meta="+boolStr(respOK))
	})

	var sessionID acp.SessionId
	t.Run("session/new", func(t *testing.T) {
		resp, err := p.client.NewSession(ctx, acp.NewSessionRequest{
			Cwd:        "/tmp",
			McpServers: []acp.McpServer{},
			Meta:       arvelMeta(),
		})
		if err != nil {
			t.Fatalf("session/new: %v", err)
		}
		sessionID = resp.SessionId
		p.agentImpl.mu.Lock()
		ok := arvelMetaOK(p.agentImpl.newMeta)
		p.agentImpl.mu.Unlock()
		record("session/new", ok, "request meta="+boolStr(ok))
	})

	t.Run("session/prompt result", func(t *testing.T) {
		p.agentImpl.mu.Lock()
		p.agentImpl.respMeta = arvelMeta()
		p.agentImpl.mu.Unlock()
		resp, err := p.client.Prompt(ctx, acp.PromptRequest{SessionId: sessionID, Prompt: []acp.ContentBlock{acp.TextBlock("hi")}})
		if err != nil {
			t.Fatalf("session/prompt: %v", err)
		}
		ok := arvelMetaOK(resp.Meta)
		record("session/prompt result", ok, "response meta="+boolStr(ok))
	})

	t.Run("session/update", func(t *testing.T) {
		update := acp.UpdateAgentMessageText("x")
		update.AgentMessageChunk.Meta = arvelMeta()
		if err := p.agent.SessionUpdate(ctx, acp.SessionNotification{SessionId: sessionID, Meta: arvelMeta(), Update: update}); err != nil {
			t.Fatalf("session/update: %v", err)
		}
		select {
		case n := <-p.clientImpl.updates:
			notifOK := arvelMetaOK(n.Meta)
			chunkOK := n.Update.AgentMessageChunk != nil && arvelMetaOK(n.Update.AgentMessageChunk.Meta)
			record("session/update", notifOK && chunkOK, "notification meta="+boolStr(notifOK)+", chunk meta="+boolStr(chunkOK))
		case <-time.After(2 * time.Second):
			record("session/update", false, "client received no update within 2s")
		}
	})

	t.Run("session/request_permission", func(t *testing.T) {
		title := "Run go test"
		req := acp.RequestPermissionRequest{
			SessionId: sessionID,
			Meta:      arvelMeta(),
			ToolCall:  acp.ToolCallUpdate{ToolCallId: "t_1", Title: &title},
			Options: []acp.PermissionOption{
				{OptionId: "allow_once", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce},
				{OptionId: "reject_once", Name: "Deny", Kind: acp.PermissionOptionKindRejectOnce},
			},
		}
		if _, err := p.agent.RequestPermission(ctx, req); err != nil {
			t.Fatalf("session/request_permission: %v", err)
		}
		select {
		case got := <-p.clientImpl.permReqs:
			ok := arvelMetaOK(got.Meta)
			record("session/request_permission", ok, "request meta="+boolStr(ok))
		case <-time.After(2 * time.Second):
			record("session/request_permission", false, "client saw no request within 2s")
		}
	})

	all := len(results) == 5
	for _, ok := range results {
		all = all && ok
	}
	logProbe(t, 3, all, "all five _meta locations preserved")
}
