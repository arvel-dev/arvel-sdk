// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	"context"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// stubProvider implements plugin.Provider for the mockhost tests.
type stubProvider struct {
	features   plugin.Features
	status     protocol.Status
	models     protocol.ModelsResult
	usage      protocol.Usage
	action     protocol.ActionResult
	newSession func(context.Context, plugin.SessionParams) (plugin.Session, error)
}

func (p *stubProvider) Initialize(context.Context, protocol.InitMeta) (plugin.Features, error) {
	return p.features, nil
}
func (p *stubProvider) Status(context.Context) (protocol.Status, error) { return p.status, nil }
func (p *stubProvider) Models(context.Context, bool) (protocol.ModelsResult, error) {
	return p.models, nil
}
func (p *stubProvider) Usage(context.Context) (protocol.Usage, error) { return p.usage, nil }
func (p *stubProvider) Action(context.Context, string) (protocol.ActionResult, error) {
	return p.action, nil
}
func (p *stubProvider) NewSession(ctx context.Context, sp plugin.SessionParams) (plugin.Session, error) {
	if p.newSession != nil {
		return p.newSession(ctx, sp)
	}
	return &stubSession{id: "s_1"}, nil
}
func (p *stubProvider) LoadSession(context.Context, plugin.LoadParams) (plugin.Session, error) {
	return &stubSession{id: "s_1"}, nil
}
func (p *stubProvider) Shutdown(context.Context) error { return nil }

// stubSession implements plugin.Session.
type stubSession struct {
	id     string
	prompt func(ctx context.Context, blocks []acp.ContentBlock, out plugin.Output) (plugin.TurnResult, error)
}

func (s *stubSession) ID() string { return s.id }
func (s *stubSession) Prompt(ctx context.Context, blocks []acp.ContentBlock, out plugin.Output) (plugin.TurnResult, error) {
	if s.prompt != nil {
		return s.prompt(ctx, blocks, out)
	}
	return plugin.TurnResult{StopReason: acp.StopReasonEndTurn}, nil
}
func (s *stubSession) SetOption(context.Context, string, string) ([]acp.SessionConfigOption, error) {
	return nil, nil
}
func (s *stubSession) Cancel()                     {}
func (s *stubSession) Close(context.Context) error { return nil }

// stubRouter implements plugin.Router and all four route interfaces.
type stubRouter struct {
	status    protocol.Status
	planFn    func(protocol.RoutePlanParams) protocol.RoutePlanResult
	messageFn func(protocol.RouteMessageParams) protocol.RouteMessageResult
	effortFn  func(protocol.RouteEffortParams) protocol.RouteEffortResult
	explainFn func(protocol.RoutePlanParams) protocol.RouteExplainResult
}

func (r *stubRouter) Initialize(context.Context, protocol.InitMeta) (plugin.RouterFeatures, error) {
	return plugin.RouterFeatures{}, nil
}
func (r *stubRouter) Status(context.Context) (protocol.Status, error) { return r.status, nil }
func (r *stubRouter) Shutdown(context.Context) error                  { return nil }
func (r *stubRouter) Plan(_ context.Context, p protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
	if r.planFn != nil {
		return r.planFn(p), nil
	}
	return protocol.RoutePlanResult{}, nil
}
func (r *stubRouter) Message(_ context.Context, p protocol.RouteMessageParams) (protocol.RouteMessageResult, error) {
	if r.messageFn != nil {
		return r.messageFn(p), nil
	}
	return protocol.RouteMessageResult{}, nil
}
func (r *stubRouter) Effort(_ context.Context, p protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
	if r.effortFn != nil {
		return r.effortFn(p), nil
	}
	return protocol.RouteEffortResult{}, nil
}
func (r *stubRouter) Explain(_ context.Context, p protocol.RoutePlanParams) (protocol.RouteExplainResult, error) {
	if r.explainFn != nil {
		return r.explainFn(p), nil
	}
	return protocol.RouteExplainResult{}, nil
}

// streamingProvider returns a provider whose session streams one chunk and
// returns end_turn.
func streamingProvider() *stubProvider {
	return &stubProvider{
		status: protocol.Status{State: protocol.StateReady},
		models: protocol.ModelsResult{Models: []protocol.Model{{ID: "m1", Label: "M1"}}, Default: "m1"},
		usage:  protocol.Usage{Source: protocol.UsageSourceUnknown, Status: protocol.UsageStatusOK},
		newSession: func(context.Context, plugin.SessionParams) (plugin.Session, error) {
			return &stubSession{id: "s_1", prompt: func(ctx context.Context, _ []acp.ContentBlock, out plugin.Output) (plugin.TurnResult, error) {
				if err := out.Update(acp.UpdateAgentMessageText("hello"), nil); err != nil {
					return plugin.TurnResult{}, err
				}
				return plugin.TurnResult{StopReason: acp.StopReasonEndTurn}, nil
			}}, nil
		},
	}
}

// permissionProvider returns a provider whose session asks one permission and
// ends the turn with the decision.
func permissionProvider() *stubProvider {
	return &stubProvider{
		status: protocol.Status{State: protocol.StateReady},
		newSession: func(context.Context, plugin.SessionParams) (plugin.Session, error) {
			return &stubSession{id: "s_1", prompt: func(ctx context.Context, _ []acp.ContentBlock, out plugin.Output) (plugin.TurnResult, error) {
				d, err := out.RequestPermission(ctx, plugin.PermissionRequest{
					ToolCall: acp.ToolCallUpdate{ToolCallId: "t_1"},
					Options: []acp.PermissionOption{
						{OptionId: "allow_once", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce},
					},
					Meta: protocol.PermissionMeta{Category: protocol.PermissionCategoryCommand, Command: "go"},
				})
				if err != nil {
					return plugin.TurnResult{}, err
				}
				if d.Cancelled {
					return plugin.TurnResult{StopReason: acp.StopReasonCancelled}, nil
				}
				return plugin.TurnResult{StopReason: acp.StopReasonEndTurn}, nil
			}}, nil
		},
	}
}
