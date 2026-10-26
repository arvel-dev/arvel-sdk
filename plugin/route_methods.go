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
		s.peer.Handle(protocol.MethodRoutePlan, routeCall(s, protocol.MethodRoutePlan, r.Plan,
			func(res protocol.RoutePlanResult, p protocol.RoutePlanParams) error { return res.Validate(p) }))
	}
	if r, ok := s.r.(MessageRouter); ok {
		s.peer.Handle(protocol.MethodRouteMessage, routeCall(s, protocol.MethodRouteMessage, r.Message,
			func(res protocol.RouteMessageResult, p protocol.RouteMessageParams) error { return res.Validate(p) }))
	}
	if r, ok := s.r.(EffortRouter); ok {
		s.peer.Handle(protocol.MethodRouteEffort, routeCall(s, protocol.MethodRouteEffort, r.Effort,
			func(res protocol.RouteEffortResult, p protocol.RouteEffortParams) error { return res.Validate(p) }))
	}
	if r, ok := s.r.(ExplainRouter); ok {
		s.peer.Handle(protocol.MethodRouteExplain, routeCall(s, protocol.MethodRouteExplain, r.Explain,
			func(res protocol.RouteExplainResult, p protocol.RoutePlanParams) error { return res.Validate(p) }))
	}
}

func routeTimeout() error {
	return protocol.NewError(protocol.ErrProviderError, "route call timed out", map[string]any{"vendorCode": "timeout"})
}

// routeCall builds the handler of one route method: it decodes the params, runs
// the router under the route timeout, validates the result against the request
// and counts the call.
func routeCall[P, R any](s *routerServer, method string, call func(context.Context, P) (R, error), check func(R, P) error) jsonrpc.HandlerFunc {
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
		res, err := runRoute(s, ctx, method, call, params, check)
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
func runRoute[P, R any](s *routerServer, ctx context.Context, method string, call func(context.Context, P) (R, error), params P, check func(R, P) error) (res R, err error) {
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
	if verr := check(res, params); verr != nil {
		s.log.Error("router returned an invalid result", "method", method, "err", verr.Error())
		return res, protocol.NewError(protocol.ErrInternal, "router returned an invalid result", nil)
	}
	return res, nil
}
