// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/arvel-dev/arvel-sdk/jsonrpc"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// routeCounter counts the calls and failures of one route method.
type routeCounter struct{ calls, failures atomic.Int64 }

// registerRoutes registers the route methods of the implemented interfaces.
func (s *routerServer) registerRoutes() {
	if r, ok := s.r.(PlanRouter); ok {
		s.peer.Handle(protocol.MethodRoutePlan, routeCall(s, protocol.MethodRoutePlan, r.Plan, s.finishPlan))
	}
	if r, ok := s.r.(MessageRouter); ok {
		s.peer.Handle(protocol.MethodRouteMessage, routeCall(s, protocol.MethodRouteMessage, r.Message,
			func(res protocol.RouteMessageResult, p protocol.RouteMessageParams) (protocol.RouteMessageResult, error) {
				return res, res.Validate(p)
			}))
	}
	if r, ok := s.r.(EffortRouter); ok {
		s.peer.Handle(protocol.MethodRouteEffort, routeCall(s, protocol.MethodRouteEffort, r.Effort,
			func(res protocol.RouteEffortResult, p protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
				return res, res.Validate(p)
			}))
	}
	if r, ok := s.r.(ExplainRouter); ok {
		s.peer.Handle(protocol.MethodRouteExplain, routeCall(s, protocol.MethodRouteExplain, r.Explain,
			func(res protocol.RouteExplainResult, p protocol.RoutePlanParams) (protocol.RouteExplainResult, error) {
				return res, res.Validate(p)
			}))
	}
}

// finishPlan validates a plan result and drops the decisions the host ignores
// (PROTOCOL §14.3), logging each one without any request text.
func (s *routerServer) finishPlan(res protocol.RoutePlanResult, p protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
	if err := res.Validate(p); err != nil {
		return res, err
	}
	clean, ignored := res.Sanitize(p)
	for _, is := range ignored {
		s.log.Warn("router decision ignored", "method", protocol.MethodRoutePlan, "path", is.Path)
	}
	return clean, nil
}

func routeTimeout() error {
	return protocol.NewError(protocol.ErrProviderError, "route call timed out", map[string]any{"vendorCode": "timeout"})
}

// routeCall builds the handler of one route method: it decodes the params, runs
// the router under the route timeout, validates the result against the request
// and counts the call.
func routeCall[P, R any](s *routerServer, method string, call func(context.Context, P) (R, error), finish func(R, P) (R, error)) jsonrpc.HandlerFunc {
	ctr := &routeCounter{}
	s.counters[method] = ctr
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		if err := s.requireInit(); err != nil {
			return nil, err
		}
		var params P
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		start := time.Now()
		res, err := runRoute(s, ctx, method, call, params, finish)
		calls := ctr.calls.Add(1)
		if err != nil {
			ctr.failures.Add(1)
		}
		s.log.Debug("route call", "method", method, "ms", time.Since(start).Milliseconds(),
			"ok", err == nil, "calls", calls, "failures", ctr.failures.Load())
		if err != nil {
			return nil, err
		}
		return res, nil
	}
}

// runRoute calls the router under the route timeout and validates the result.
func runRoute[P, R any](s *routerServer, ctx context.Context, method string, call func(context.Context, P) (R, error), params P, finish func(R, P) (R, error)) (res R, err error) {
	rctx, cancel := context.WithTimeout(ctx, s.o.routeTimeout)
	defer cancel()

	var callErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("router panic", "method", method, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				callErr = protocol.NewError(protocol.ErrInternal, "internal error", nil)
			}
		}()
		res, callErr = call(rctx, params)
	}()

	var re *protocol.RPCError
	switch {
	case callErr != nil && !errors.As(callErr, &re) && errors.Is(callErr, context.DeadlineExceeded):
		return res, routeTimeout()
	case callErr != nil:
		return res, s.rpcError(callErr)
	case rctx.Err() != nil: // the router answered after the host gave up
		return res, routeTimeout()
	}
	out, verr := finish(res, params)
	if verr != nil {
		s.log.Error("router returned an invalid result", "method", method, "err", verr.Error())
		return res, protocol.NewError(protocol.ErrInternal, "router returned an invalid result", nil)
	}
	return out, nil
}
