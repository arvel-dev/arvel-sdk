// SPDX-License-Identifier: MIT-0

package main

import (
	"context"
	"strings"
	"sync/atomic"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// session implements plugin.Session. It streams the prompt back word by word,
// asks one permission when the prompt contains !run, and stops promptly when
// cancelled.
type session struct {
	id     string
	cancel atomic.Bool
}

func (s *session) ID() string { return s.id }

func (s *session) Prompt(ctx context.Context, blocks []acp.ContentBlock, out plugin.Output) (plugin.TurnResult, error) {
	var text string
	for _, b := range blocks {
		if b.Text != nil {
			text += b.Text.Text
		}
	}
	s.cancel.Store(false)
	words := strings.Fields(text)
	for _, w := range words {
		if s.cancel.Load() {
			return plugin.TurnResult{StopReason: acp.StopReasonCancelled}, nil
		}
		select {
		case <-ctx.Done():
			return plugin.TurnResult{StopReason: acp.StopReasonCancelled}, nil
		default:
		}
		if err := out.Update(acp.UpdateAgentMessageText(w+" "), nil); err != nil {
			return plugin.TurnResult{}, err
		}
	}
	if strings.Contains(text, "!run") {
		d, err := out.RequestPermission(ctx, plugin.PermissionRequest{
			ToolCall: acp.ToolCallUpdate{ToolCallId: "t_1", RawInput: map[string]any{"command": "go test ./..."}},
			Options: []acp.PermissionOption{
				{OptionId: "allow_once", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce},
				{OptionId: "reject_once", Name: "Deny", Kind: acp.PermissionOptionKindRejectOnce},
			},
			Meta: protocol.PermissionMeta{Category: protocol.PermissionCategoryCommand, Command: "go test ./...", Reason: "!run in the prompt"},
		})
		if err != nil {
			return plugin.TurnResult{}, err
		}
		if d.Cancelled {
			return plugin.TurnResult{StopReason: acp.StopReasonCancelled}, nil
		}
		if d.OptionID == "reject_once" {
			_ = out.Update(acp.UpdateAgentMessageText("denied"), nil)
		} else {
			_ = out.Update(acp.UpdateAgentMessageText("ran go test ./..."), nil)
		}
	}
	return plugin.TurnResult{
		StopReason: acp.StopReasonEndTurn,
		Usage: &protocol.TurnUsage{
			InputTokens:  int64(len(text)),
			OutputTokens: int64(len(words)),
			CostSource:   protocol.UsageSourceUnknown,
		},
	}, nil
}

func (s *session) SetOption(context.Context, string, string) ([]acp.SessionConfigOption, error) {
	return nil, nil
}

func (s *session) Cancel() { s.cancel.Store(true) }

func (s *session) Close(context.Context) error { return nil }
