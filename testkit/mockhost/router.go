// SPDX-License-Identifier: Apache-2.0

package mockhost

import "github.com/arvel-dev/arvel-sdk/protocol"

// RoutePlan calls _arvel/route/plan.
func (h *Host) RoutePlan(p protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
	var res protocol.RoutePlanResult
	err := h.call(protocol.MethodRoutePlan, p, &res)
	return res, err
}

// RouteMessage calls _arvel/route/message.
func (h *Host) RouteMessage(p protocol.RouteMessageParams) (protocol.RouteMessageResult, error) {
	var res protocol.RouteMessageResult
	err := h.call(protocol.MethodRouteMessage, p, &res)
	return res, err
}

// RouteEffort calls _arvel/route/effort.
func (h *Host) RouteEffort(p protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
	var res protocol.RouteEffortResult
	err := h.call(protocol.MethodRouteEffort, p, &res)
	return res, err
}

// RouteExplain calls _arvel/route/explain.
func (h *Host) RouteExplain(p protocol.RoutePlanParams) (protocol.RouteExplainResult, error) {
	var res protocol.RouteExplainResult
	err := h.call(protocol.MethodRouteExplain, p, &res)
	return res, err
}
