// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

var validStopReasons = map[acp.StopReason]bool{
	acp.StopReasonEndTurn:         true,
	acp.StopReasonMaxTokens:       true,
	acp.StopReasonMaxTurnRequests: true,
	acp.StopReasonRefusal:         true,
	acp.StopReasonCancelled:       true,
}

func (s *providerServer) prompt(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireInit(); err != nil {
		return nil, err
	}
	var req acp.PromptRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, invalidParams("%v", err)
	}
	if !s.beginTurn() {
		return nil, s.shuttingDown()
	}
	defer s.endTurn()

	id := string(req.SessionId)
	for attempt := 0; ; attempt++ {
		e, err := s.acquire(ctx, id)
		if err != nil {
			return nil, err
		}
		// The turn runs under the server context, so shutdown ends it, and under
		// the request context, so a closed connection ends it too.
		tctx, cancelTurn := context.WithCancel(s.ctx)
		turn, ok := e.tryStart(tctx)
		if !ok {
			cancelTurn()
			if attempt == 0 && e.isClosing() { // the reaper got there first
				continue
			}
			return nil, protocol.NewError(protocol.ErrBusy, "session already has a running turn", nil)
		}
		stop := context.AfterFunc(ctx, cancelTurn)
		res, err := s.runTurn(tctx, e, turn, req.Prompt)
		stop()
		cancelTurn()
		return res, err
	}
}

// runTurn runs the provider's Prompt and maps its outcome to the response.
func (s *providerServer) runTurn(ctx context.Context, e *sessionEntry, turn *turnState, blocks []acp.ContentBlock) (any, error) {
	defer func() { e.finish(s.o.clock.Now()) }() // Now at the end of the turn, not at its start
	out := &output{s: s, sessionID: e.id, permCtx: turn.permCtx}

	res, err := s.callPrompt(ctx, e, blocks, out)

	e.mu.Lock()
	cancelled := turn.cancelled
	e.mu.Unlock()
	switch {
	case err != nil && cancelled:
		s.log.Debug("turn ended with an error after cancel", "session", e.id, "err", s.redactor.String(err.Error()))
		res = TurnResult{StopReason: acp.StopReasonCancelled}
	case err != nil:
		return nil, s.rpcError(err)
	}
	if !validStopReasons[res.StopReason] {
		s.log.Error("provider returned an invalid stop reason", "session", e.id, "stopReason", string(res.StopReason))
		return nil, protocol.NewError(protocol.ErrInternal, "provider returned an invalid stop reason", nil)
	}
	if res.Stop != nil {
		if verr := res.Stop.Validate(); verr != nil {
			return nil, s.invalidResult("stop detail", verr)
		}
	}
	resp := acp.PromptResponse{StopReason: res.StopReason}
	if res.Usage != nil || res.Stop != nil {
		resp.Meta = protocol.WithMeta(nil, protocol.TurnMeta{Usage: res.Usage, Stop: res.Stop})
	}
	return resp, nil
}

// callPrompt calls Session.Prompt; a panic becomes an internal error.
func (s *providerServer) callPrompt(ctx context.Context, e *sessionEntry, blocks []acp.ContentBlock, out Output) (res TurnResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("provider panic", "what", "Session.Prompt", "session", e.id,
				"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			res, err = TurnResult{}, protocol.NewError(protocol.ErrInternal, "internal error", nil)
		}
	}()
	return e.sess.Prompt(ctx, blocks, out)
}

// cancel handles the session/cancel notification.
func (s *providerServer) cancel(_ context.Context, raw json.RawMessage) {
	var req acp.CancelNotification
	if err := decodeParams(raw, &req); err != nil {
		s.log.Debug("invalid session/cancel ignored")
		return
	}
	e, ok := s.reg.get(string(req.SessionId))
	if !ok {
		s.log.Debug("session/cancel for an unknown session ignored", "session", string(req.SessionId))
		return
	}
	e.requestCancel(s)
}

// requestCancel asks the running turn to stop: it calls Session.Cancel, cancels
// the pending permission requests and arms the watchdog that warns when the turn
// still runs after CancelWarnAfter. The framework keeps waiting; the provider
// owns killing a stuck vendor process.
func (e *sessionEntry) requestCancel(s *providerServer) {
	e.mu.Lock()
	t := e.turn
	if !e.running || t == nil {
		e.mu.Unlock()
		return
	}
	t.cancelled = true
	e.mu.Unlock()

	s.safely(e.id, "Session.Cancel", e.sess.Cancel)
	t.permCancel()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.turn == t && t.watchdog == nil {
		id := e.id
		t.watchdog = s.o.clock.AfterFunc(CancelWarnAfter, func() {
			s.log.Warn("prompt still running after cancel", "session", id, "seconds", int(CancelWarnAfter.Seconds()))
		})
	}
}
