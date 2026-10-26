// SPDX-License-Identifier: MIT-0

package main

import (
	"context"

	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// router implements plugin.Router and the four optional route interfaces.
type router struct{}

func (router) Initialize(context.Context, protocol.InitMeta) (plugin.RouterFeatures, error) {
	return plugin.RouterFeatures{}, nil
}

func (router) Status(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateReady, Message: "Local keyword rules"}, nil
}

func (router) Shutdown(context.Context) error { return nil }

func (router) Plan(_ context.Context, p protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
	res, _ := planDecision(p)
	return res, nil
}

func (router) Message(_ context.Context, p protocol.RouteMessageParams) (protocol.RouteMessageResult, error) {
	return messageDecision(p)
}

func (router) Effort(_ context.Context, p protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
	return effortDecision(p)
}

func (router) Explain(_ context.Context, p protocol.RoutePlanParams) (protocol.RouteExplainResult, error) {
	_, lines := planDecision(p)
	return protocol.RouteExplainResult{Lines: lines}, nil
}
