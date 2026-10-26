// SPDX-License-Identifier: Apache-2.0

package spike

import (
	"context"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

func permissionRequest(session acp.SessionId) acp.RequestPermissionRequest {
	title := "Run go test ./..."
	return acp.RequestPermissionRequest{
		SessionId: session,
		ToolCall:  acp.ToolCallUpdate{ToolCallId: "t_1", Title: &title},
		Options: []acp.PermissionOption{
			{OptionId: "allow_once", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce},
			{OptionId: "reject_once", Name: "Deny", Kind: acp.PermissionOptionKindRejectOnce},
		},
	}
}

func textPrompt(session acp.SessionId) acp.PromptRequest {
	return acp.PromptRequest{SessionId: session, Prompt: []acp.ContentBlock{acp.TextBlock("hi")}}
}

// Probe 5 (SPEC §5.3 point 5): the agent sends session/request_permission and
// waits without a deadline while another session keeps working.
func TestProbe5_UnboundedPermissionWaitsWhileOthersContinue(t *testing.T) {
	delay := 15 * time.Second
	if testing.Short() {
		delay = 2 * time.Second
	}

	p := newPair(t)
	release := make(chan struct{})
	p.clientImpl.permissionFn = func(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		select {
		case <-release:
			return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(req.Options[0].OptionId)}, nil
		case <-ctx.Done():
			return acp.RequestPermissionResponse{}, ctx.Err()
		}
	}

	outcome := make(chan string, 1)
	p.agentImpl.promptFn = func(_ context.Context, req acp.PromptRequest) (acp.PromptResponse, error) {
		if req.SessionId == "s_1" {
			// No deadline on purpose: this is what the plugin framework must do.
			resp, err := p.agent.RequestPermission(context.Background(), permissionRequest(req.SessionId))
			switch {
			case err != nil:
				outcome <- "error: " + err.Error()
				return acp.PromptResponse{}, err
			case resp.Outcome.Selected != nil:
				outcome <- "selected " + string(resp.Outcome.Selected.OptionId)
			default:
				outcome <- "not selected"
			}
		}
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), delay+30*time.Second)
	defer cancel()

	s1done := make(chan error, 1)
	go func() {
		_, err := p.client.Prompt(ctx, textPrompt("s_1"))
		s1done <- err
	}()

	select {
	case <-p.clientImpl.permReqs:
		t.Log("client received the permission request for s_1")
	case <-time.After(5 * time.Second):
		t.Fatal("permission request never reached the client")
	}

	start := time.Now()
	s2ctx, s2cancel := context.WithTimeout(ctx, 5*time.Second)
	defer s2cancel()
	_, s2err := p.client.Prompt(s2ctx, textPrompt("s_2"))
	s2took := time.Since(start)
	t.Logf("session s_2 prompt finished in %s err=%v while s_1 permission is pending", s2took, s2err)
	concurrent := s2err == nil && s2took < time.Second

	t.Logf("holding the permission answer for %s", delay)
	time.Sleep(delay)

	waitedOK := true
	select {
	case err := <-s1done:
		waitedOK = false
		t.Logf("s_1 finished before the answer was released: err=%v", err)
	default:
	}

	close(release)
	var finalErr error
	select {
	case finalErr = <-s1done:
	case <-time.After(10 * time.Second):
		finalErr = context.DeadlineExceeded
		waitedOK = false
	}
	got := <-outcome
	t.Logf("s_1 final: err=%v agent saw outcome=%q", finalErr, got)

	yes := waitedOK && finalErr == nil && got == "selected allow_once" && concurrent
	logProbe(t, 5, yes, "pending for "+delay.String()+" without timeout="+boolStr(waitedOK && finalErr == nil)+
		", other session unblocked="+boolStr(concurrent))
}

// Extra observation for probe 5: cancelling the agent's context ends the
// pending permission call, and the client learns about it.
func TestProbe5_CancelEndsPendingPermission(t *testing.T) {
	p := newPair(t)

	clientView := make(chan string, 1)
	p.clientImpl.permissionFn = func(ctx context.Context, _ acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		select {
		case <-ctx.Done():
			clientView <- "client handler context cancelled"
		case <-time.After(1500 * time.Millisecond):
			clientView <- "client handler context NOT cancelled within 1.5s"
		}
		return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
	}

	type result struct {
		err error
		at  time.Time
	}
	cancelCh := make(chan context.CancelFunc, 1)
	agentResult := make(chan result, 1)
	p.agentImpl.promptFn = func(_ context.Context, req acp.PromptRequest) (acp.PromptResponse, error) {
		cctx, cancel := context.WithCancel(context.Background())
		cancelCh <- cancel
		_, err := p.agent.RequestPermission(cctx, permissionRequest(req.SessionId))
		agentResult <- result{err: err, at: time.Now()}
		return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go func() { _, _ = p.client.Prompt(ctx, textPrompt("s_1")) }()

	cancelPerm := <-cancelCh
	select {
	case <-p.clientImpl.permReqs:
	case <-time.After(5 * time.Second):
		t.Fatal("permission request never reached the client")
	}

	t0 := time.Now()
	cancelPerm()
	select {
	case r := <-agentResult:
		took := r.at.Sub(t0)
		t.Logf("agent RequestPermission returned after %s err=%v", took, r.err)
		t.Logf("OBSERVATION permission-cancel: agent returned error=%v within 1s=%v", r.err != nil, took <= time.Second)
	case <-time.After(5 * time.Second):
		t.Log("OBSERVATION permission-cancel: agent call did not return within 5s after cancel")
	}
	select {
	case v := <-clientView:
		t.Logf("OBSERVATION permission-cancel: %s", v)
	case <-time.After(3 * time.Second):
		t.Log("OBSERVATION permission-cancel: client handler never finished")
	}
}
