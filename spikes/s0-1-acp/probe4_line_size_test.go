// SPDX-License-Identifier: Apache-2.0

package spike

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

const (
	dirClientToAgent = "client->agent"
	dirAgentToClient = "agent->client"
	mib              = 1 << 20
	// envelopeHeadroom is the room left for the JSON-RPC envelope around the text.
	envelopeHeadroom = 4096
)

// Probe 4 (SPEC §5.3 point 4): lines up to 64 MiB are accepted in both
// directions. A fresh pair is used for every size and direction because a
// failing reader may kill the connection.
func TestProbe4_LargeLines(t *testing.T) {
	if testing.Short() {
		t.Skip("probe 4 holds up to 64 MiB payloads; skipped in -short mode")
	}

	sizes := []int{1 * mib, 9 * mib, 11 * mib, 32 * mib, 64 * mib}
	best := map[string]int{dirClientToAgent: 0, dirAgentToClient: 0}

	for _, size := range sizes {
		for _, dir := range []string{dirClientToAgent, dirAgentToClient} {
			ok, detail := runLargeLine(t, size, dir)
			t.Logf("size=%d MiB direction=%s ok=%v %s", size/mib, dir, ok, detail)
			if ok && size > best[dir] {
				best[dir] = size
			}
		}
	}

	yes := best[dirClientToAgent] >= 64*mib && best[dirAgentToClient] >= 64*mib
	logProbe(t, 4, yes, fmt.Sprintf("largest working line: %s=%d MiB, %s=%d MiB (required 64 MiB)",
		dirClientToAgent, best[dirClientToAgent]/mib, dirAgentToClient, best[dirAgentToClient]/mib))
}

func runLargeLine(t *testing.T, size int, dir string) (bool, string) {
	t.Helper()
	p := newPair(t)
	defer p.close()

	text := strings.Repeat("a", size-envelopeHeadroom)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p.agentImpl.setExt("_arvel/status", func(context.Context, json.RawMessage) (any, error) {
		return map[string]any{"state": "ready"}, nil
	})

	var (
		ok     bool
		detail string
	)
	switch dir {
	case dirClientToAgent:
		ok, detail = sendPrompt(ctx, p, text, size)
	default:
		ok, detail = sendUpdate(ctx, p, text)
	}
	if !ok {
		detail += "; follow-up call: " + followUp(p)
	}
	return ok, detail
}

func sendPrompt(ctx context.Context, p *pair, text string, size int) (bool, string) {
	req := acp.PromptRequest{SessionId: "s_1", Prompt: []acp.ContentBlock{acp.TextBlock(text)}}
	if raw, err := json.Marshal(req); err == nil && (len(raw) < size-2*envelopeHeadroom || len(raw) > size) {
		return false, fmt.Sprintf("test setup: marshalled request is %d bytes for size %d", len(raw), size)
	}

	seen := make(chan int, 1)
	p.agentImpl.promptFn = func(_ context.Context, r acp.PromptRequest) (acp.PromptResponse, error) {
		seen <- len(r.Prompt[0].Text.Text)
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := p.client.Prompt(ctx, req)
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return false, "prompt returned error: " + truncateStr(err.Error(), 200)
		}
		if got := <-seen; got != len(text) {
			return false, fmt.Sprintf("agent saw %d bytes, sent %d", got, len(text))
		}
		return true, ""
	case <-p.agent.Done():
		return false, "agent side connection closed (reader gave up)"
	case <-p.client.Done():
		return false, "client side connection closed"
	case <-ctx.Done():
		return false, "no response within 30s (silent drop or stall)"
	}
}

func sendUpdate(ctx context.Context, p *pair, text string) (bool, string) {
	notif := acp.SessionNotification{SessionId: "s_1", Update: acp.UpdateAgentMessageText(text)}

	errCh := make(chan error, 1)
	go func() { errCh <- p.agent.SessionUpdate(ctx, notif) }()

	select {
	case n := <-p.clientImpl.updates:
		if n.Update.AgentMessageChunk == nil || n.Update.AgentMessageChunk.Content.Text == nil {
			return false, "client received an update without text"
		}
		if got := len(n.Update.AgentMessageChunk.Content.Text.Text); got != len(text) {
			return false, fmt.Sprintf("client saw %d bytes, sent %d", got, len(text))
		}
		return true, ""
	case err := <-errCh:
		if err != nil {
			return false, "SessionUpdate returned error: " + truncateStr(err.Error(), 200)
		}
		// The notification was written; wait for the client to receive it.
		select {
		case n := <-p.clientImpl.updates:
			if n.Update.AgentMessageChunk == nil || n.Update.AgentMessageChunk.Content.Text == nil ||
				len(n.Update.AgentMessageChunk.Content.Text.Text) != len(text) {
				return false, "client received a damaged update"
			}
			return true, ""
		case <-p.client.Done():
			return false, "client side connection closed (reader gave up)"
		case <-ctx.Done():
			return false, "notification sent but never received within 30s (silent drop)"
		}
	case <-p.client.Done():
		return false, "client side connection closed (reader gave up)"
	case <-ctx.Done():
		return false, "no delivery within 30s"
	}
}

// followUp tries a small call to learn whether the connection survived.
func followUp(p *pair) string {
	res := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := p.client.CallExtension(ctx, "_arvel/status", map[string]any{})
		res <- err
	}()
	select {
	case err := <-res:
		if err != nil {
			return "connection unusable (" + truncateStr(err.Error(), 120) + ")"
		}
		return "connection still usable"
	case <-time.After(3 * time.Second):
		return "connection unusable (call blocked)"
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
