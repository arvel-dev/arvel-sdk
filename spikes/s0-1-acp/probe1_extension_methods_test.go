// SPDX-License-Identifier: Apache-2.0

package spike

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// Probe 1 (SPEC §5.3 point 1): the agent receives and answers custom methods
// with the "_" prefix, and unknown ones come back as -32601.
func TestProbe1_AgentReceivesCustomMethod(t *testing.T) {
	p := newPair(t)
	p.agentImpl.setExt("_arvel/status", func(context.Context, json.RawMessage) (any, error) {
		return map[string]any{"state": "ready"}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	raw, err := p.client.CallExtension(ctx, "_arvel/status", map[string]any{})
	if err != nil {
		logProbe(t, 1, false, "_arvel/status call failed: "+err.Error())
		return
	}
	var got struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode _arvel/status result %q: %v", truncate(raw, 512), err)
	}
	t.Logf("_arvel/status result: %s", truncate(raw, 512))
	statusOK := got.State == "ready"

	_, err = p.client.CallExtension(ctx, "_arvel/nope", nil)
	code := 0
	var re *acp.RequestError
	if errors.As(err, &re) {
		code = re.Code
	}
	t.Logf("unknown _arvel/nope: err=%v code=%d", err, code)
	unknownOK := err != nil && code == -32601

	logProbe(t, 1, statusOK && unknownOK, "custom method answered="+boolStr(statusOK)+", unknown _arvel/* code -32601="+boolStr(unknownOK))
}

// Probe 2 (SPEC §5.3 point 2): the agent can send custom notifications.
func TestProbe2_AgentSendsCustomNotification(t *testing.T) {
	p := newPair(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.agent.NotifyExtension(ctx, "_arvel/usage_updated", map[string]any{"source": "exact"}); err != nil {
		logProbe(t, 2, false, "NotifyExtension failed: "+err.Error())
		return
	}

	select {
	case call := <-p.clientImpl.extNotify:
		t.Logf("client received %s %s", call.Method, truncate(call.Params, 512))
		var body struct {
			Source string `json:"source"`
		}
		_ = json.Unmarshal(call.Params, &body)
		logProbe(t, 2, call.Method == "_arvel/usage_updated" && body.Source == "exact", "client received method="+call.Method)
	case <-time.After(2 * time.Second):
		logProbe(t, 2, false, "client did not receive the notification within 2s")
	}
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
