// SPDX-License-Identifier: Apache-2.0

package mockhost

import "github.com/arvel-dev/arvel-sdk/protocol"

// RoutePlan calls _arvel/route/plan.
func (c *core) RoutePlan(p protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
	var res protocol.RoutePlanResult
	err := c.call(protocol.MethodRoutePlan, p, &res)
	return res, err
}

// RouteMessage calls _arvel/route/message.
func (c *core) RouteMessage(p protocol.RouteMessageParams) (protocol.RouteMessageResult, error) {
	var res protocol.RouteMessageResult
	err := c.call(protocol.MethodRouteMessage, p, &res)
	return res, err
}

// RouteEffort calls _arvel/route/effort.
func (c *core) RouteEffort(p protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
	var res protocol.RouteEffortResult
	err := c.call(protocol.MethodRouteEffort, p, &res)
	return res, err
}

// RouteExplain calls _arvel/route/explain.
func (c *core) RouteExplain(p protocol.RoutePlanParams) (protocol.RouteExplainResult, error) {
	var res protocol.RouteExplainResult
	err := c.call(protocol.MethodRouteExplain, p, &res)
	return res, err
}
