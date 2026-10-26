// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// stubRouter implements the required Router methods. Unset function fields give
// canned answers from the golden files. The optional interfaces are added by
// the wrapper types below, because Go cannot remove methods from a type.
type stubRouter struct {
	t *testing.T

	initialize func(ctx context.Context, in protocol.InitMeta) (RouterFeatures, error)
	status     func(ctx context.Context) (protocol.Status, error)
	plan       func(ctx context.Context, p protocol.RoutePlanParams) (protocol.RoutePlanResult, error)
	message    func(ctx context.Context, p protocol.RouteMessageParams) (protocol.RouteMessageResult, error)
	effort     func(ctx context.Context, p protocol.RouteEffortParams) (protocol.RouteEffortResult, error)
	explain    func(ctx context.Context, p protocol.RoutePlanParams) (protocol.RouteExplainResult, error)
	shutdown   func(ctx context.Context) error

	mu       sync.Mutex
	host     RouterHost
	lastInit protocol.InitMeta

	shutdownN atomic.Int32
}

func newStubRouter(t *testing.T) *stubRouter { return &stubRouter{t: t} }

func (r *stubRouter) SetHost(h RouterHost) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.host = h
}

func (r *stubRouter) getHost() RouterHost {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.host
}

func (r *stubRouter) Initialize(ctx context.Context, in protocol.InitMeta) (RouterFeatures, error) {
	r.mu.Lock()
	r.lastInit = in
	r.mu.Unlock()
	if r.initialize != nil {
		return r.initialize(ctx, in)
	}
	return RouterFeatures{}, nil
}

func (r *stubRouter) Status(ctx context.Context) (protocol.Status, error) {
	if r.status != nil {
		return r.status(ctx)
	}
	return protocol.Status{State: protocol.StateReady, Message: "ok"}, nil
}

func (r *stubRouter) Shutdown(ctx context.Context) error {
	r.shutdownN.Add(1)
	if r.shutdown != nil {
		return r.shutdown(ctx)
	}
	return nil
}

func (r *stubRouter) doPlan(ctx context.Context, p protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
	if r.plan != nil {
		return r.plan(ctx, p)
	}
	return goldenAs[protocol.RoutePlanResult](r.t, "14.3-route-plan-result.json", "/result"), nil
}

func (r *stubRouter) doMessage(ctx context.Context, p protocol.RouteMessageParams) (protocol.RouteMessageResult, error) {
	if r.message != nil {
		return r.message(ctx, p)
	}
	return goldenAs[protocol.RouteMessageResult](r.t, "14.4-route-message-result.json", ""), nil
}

func (r *stubRouter) doEffort(ctx context.Context, p protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
	if r.effort != nil {
		return r.effort(ctx, p)
	}
	return goldenAs[protocol.RouteEffortResult](r.t, "14.5-route-effort-result.json", ""), nil
}

func (r *stubRouter) doExplain(ctx context.Context, p protocol.RoutePlanParams) (protocol.RouteExplainResult, error) {
	if r.explain != nil {
		return r.explain(ctx, p)
	}
	return goldenAs[protocol.RouteExplainResult](r.t, "14.8-route-explain-result.json", ""), nil
}

// Wrappers: each adds a set of optional interfaces to *stubRouter.
type (
	routerNone        struct{ *stubRouter }
	routerPlan        struct{ *stubRouter }
	routerPlanMessage struct{ *stubRouter }
	routerEffort      struct{ *stubRouter }
	routerAll         struct{ *stubRouter }
)

func (r routerPlan) Plan(ctx context.Context, p protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
	return r.doPlan(ctx, p)
}

func (r routerPlanMessage) Plan(ctx context.Context, p protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
	return r.doPlan(ctx, p)
}

func (r routerPlanMessage) Message(ctx context.Context, p protocol.RouteMessageParams) (protocol.RouteMessageResult, error) {
	return r.doMessage(ctx, p)
}

func (r routerEffort) Effort(ctx context.Context, p protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
	return r.doEffort(ctx, p)
}

func (r routerAll) Plan(ctx context.Context, p protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
	return r.doPlan(ctx, p)
}

func (r routerAll) Message(ctx context.Context, p protocol.RouteMessageParams) (protocol.RouteMessageResult, error) {
	return r.doMessage(ctx, p)
}

func (r routerAll) Effort(ctx context.Context, p protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
	return r.doEffort(ctx, p)
}

func (r routerAll) Explain(ctx context.Context, p protocol.RoutePlanParams) (protocol.RouteExplainResult, error) {
	return r.doExplain(ctx, p)
}
