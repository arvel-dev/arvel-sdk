// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"fmt"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/jsonrpc"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// output is the Output of one turn.
type output struct {
	s         *providerServer
	sessionID string
	permCtx   context.Context
}

// Update sends session/update. The Arvel metadata goes into the _meta of the
// notification params, the level the S0.1 probe verified.
func (o *output) Update(u acp.SessionUpdate, meta *protocol.UpdateMeta) error {
	n := acp.SessionNotification{SessionId: acp.SessionId(o.sessionID), Update: u}
	if meta != nil {
		n.Meta = protocol.WithMeta(nil, *meta)
	}
	if err := o.s.peer.Notify(protocol.MethodSessionUpdate, n); err != nil {
		return fmt.Errorf("plugin: send update: %w", err)
	}
	return nil
}

// RequestPermission asks the host and waits without a timeout (PROTOCOL §7.3).
// It ends when the host answers, the session is cancelled, ctx ends or the
// connection closes. A malformed or failed answer is a rejection, never an
// approval.
func (o *output) RequestPermission(ctx context.Context, req PermissionRequest) (PermissionDecision, error) {
	if err := req.Meta.Validate(); err != nil {
		return PermissionDecision{}, fmt.Errorf("plugin: permission request: %w", err)
	}
	if len(req.Options) == 0 {
		return PermissionDecision{}, errors.New("plugin: permission request without options")
	}
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(o.permCtx, cancel)
	defer stop()

	s := o.s
	var resp acp.RequestPermissionResponse
	err := s.peer.Call(callCtx, protocol.MethodSessionRequestPermission, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(o.sessionID),
		ToolCall:  req.ToolCall,
		Options:   req.Options,
		Meta:      protocol.WithMeta(nil, req.Meta),
	}, &resp)
	if err != nil {
		var re *protocol.RPCError
		switch {
		case o.permCtx.Err() != nil:
			return PermissionDecision{Cancelled: true}, nil
		case ctx.Err() != nil:
			return PermissionDecision{}, ctx.Err()
		case errors.Is(err, jsonrpc.ErrClosed):
			return PermissionDecision{}, err
		case errors.As(err, &re):
			s.log.Warn("host failed the permission request", "session", o.sessionID, "code", re.Code)
			return PermissionDecision{Cancelled: true}, nil
		}
		return PermissionDecision{}, fmt.Errorf("plugin: permission request: %w", err)
	}
	switch {
	case resp.Outcome.Selected != nil && hasOption(req.Options, string(resp.Outcome.Selected.OptionId)):
		return PermissionDecision{OptionID: string(resp.Outcome.Selected.OptionId)}, nil
	case resp.Outcome.Cancelled != nil:
		return PermissionDecision{Cancelled: true}, nil
	}
	s.log.Warn("unusable permission answer treated as rejection", "session", o.sessionID)
	return PermissionDecision{Cancelled: true}, nil
}

func hasOption(opts []acp.PermissionOption, id string) bool {
	for _, o := range opts {
		if string(o.OptionId) == id {
			return true
		}
	}
	return false
}
