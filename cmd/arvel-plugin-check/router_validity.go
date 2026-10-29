// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/testkit/mockhost"
)

// The §14.9 release validity check parameters. They are variables (not
// constants) so tests can shorten the slow paths; the tests in this package
// never run in parallel.
var (
	routeDeadline      = 3 * time.Second
	randomizedRequests = 100
)

// checkRouteValidity starts a fresh driver, initializes it and runs the
// randomized id/slot/confidence validity check (PROTOCOL §14.9).
func checkRouteValidity(ctx context.Context, f *flags) (result CheckResult) {
	r, err := prepareCheck(ctx, f, "route-validity", nil, resolveFakevendorDir(f.scenarios), "")
	if err != nil {
		return fail("route validity", "start: "+err.Error())
	}
	defer r.finishOne(&result)
	d := r.d
	init, err := r.initialize()
	if err != nil {
		return fail("route validity", "initialize: "+safeError(err))
	}
	rng := rand.New(rand.NewSource(1)) // seeded for reproducibility
	return checkRouteValidityRequests(ctx, d, rng, init.Features)
}

// checkRouteValidityRequests sends randomizedRequests route requests and asserts
// each response is valid per the protocol validators (ids/slots/efforts from
// the request, confidence in [0, 1], finite explain values). A single invalid
// response is collected, not fatal; every request is tried unless the transport
// itself dies.
func checkRouteValidityRequests(ctx context.Context, d *mockhost.Driver, rng *rand.Rand, features []string) CheckResult {
	var methods []string
	for _, feature := range features {
		if isRouterFeature(feature) {
			methods = append(methods, "_arvel/"+strings.ReplaceAll(feature, ".", "/"))
		}
	}
	if len(methods) == 0 {
		return fail("route validity", "no route features announced")
	}
	var violations []string
	for i := 0; i < randomizedRequests; i++ {
		name, err := oneRandomRoute(ctx, d, rng, methods[i%len(methods)])
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s #%d: %v", name, i+1, err))
		}
	}
	if len(violations) == 0 {
		return pass("route validity", fmt.Sprintf("%d requests valid", randomizedRequests))
	}
	const max = 3
	if len(violations) > max {
		violations = append(violations[:max], fmt.Sprintf("and %d more", len(violations)-max))
	}
	return fail("route validity", strings.Join(violations, "; "))
}

// oneRandomRoute sends one randomized route request (cycling through the four
// route methods) and returns its method label and any validation or deadline
// error.
func oneRandomRoute(ctx context.Context, d *mockhost.Driver, rng *rand.Rand, method string) (string, error) {
	switch method {
	case protocol.MethodRoutePlan:
		req := randomPlan(rng)
		var res protocol.RoutePlanResult
		if err := callRoute(ctx, d, protocol.MethodRoutePlan, req, &res); err != nil {
			return "route/plan", err
		}
		return "route/plan", res.Validate(req)
	case protocol.MethodRouteMessage:
		req := randomMessage(rng)
		var res protocol.RouteMessageResult
		if err := callRoute(ctx, d, protocol.MethodRouteMessage, req, &res); err != nil {
			return "route/message", err
		}
		return "route/message", res.Validate(req)
	case protocol.MethodRouteEffort:
		req := randomEffort(rng)
		var res protocol.RouteEffortResult
		if err := callRoute(ctx, d, protocol.MethodRouteEffort, req, &res); err != nil {
			return "route/effort", err
		}
		return "route/effort", res.Validate(req)
	default:
		req := randomPlan(rng)
		var res protocol.RouteExplainResult
		if err := callRoute(ctx, d, protocol.MethodRouteExplain, req, &res); err != nil {
			return "route/explain", err
		}
		return "route/explain", res.Validate(req)
	}
}

// callRoute sends one route request under the host-side deadline. The bound is
// independent of the plugin framework's own route timeout: a plugin that
// answers late still fails the check.
func callRoute(ctx context.Context, d *mockhost.Driver, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(ctx, routeDeadline)
	defer cancel()
	if err := d.Call(ctx, method, params, result); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("exceeded %s", routeDeadline)
		}
		return err
	}
	return nil
}

// The fixed roster the randomized requests draw from. Every combination stays
// within what PROTOCOL §14 defines so a conformant router can always answer.
var (
	routeRoles = []protocol.RouteRole{
		{Slot: protocol.SlotDeveloper, RoleID: protocol.RoleDeveloper, Name: "Developer", Description: "Writes code by the plan", Kind: protocol.RoleKindCode},
		{Slot: protocol.SlotSecurity, RoleID: protocol.RoleSecurity, Name: "Security", Description: "Checks authentication", Kind: protocol.RoleKindCheck},
		{Slot: protocol.SlotTester, RoleID: protocol.RoleTester, Name: "Tester", Description: "Runs tests and browser checks", Kind: protocol.RoleKindCheck},
		{Slot: protocol.SlotLeadAccept, RoleID: protocol.RoleLead, Name: "Lead", Description: "Accepts the result", Kind: protocol.RoleKindAccept},
	}
	routeCandidates = []protocol.RouteCandidate{
		{RoleID: protocol.RoleTester, Name: "Tester", Kind: protocol.RoleKindCheck},
		{RoleID: protocol.RoleDeveloper, Name: "Developer", Kind: protocol.RoleKindCode},
		{RoleID: protocol.RoleSecurity, Name: "Security", Kind: protocol.RoleKindCheck},
	}
	routeTasks = []protocol.RouteTask{
		{Title: "Add a feature flag", Description: "Introduce a toggle behind a config"},
		{Title: "Fix the login crash", Description: "The app crashes on an empty password"},
		{Title: "Write unit tests", Description: "Cover the parser edge cases"},
	}
	routeMessages = []string{
		"ask the tester", "the developer should look", "check the security implications", "please accept the result",
	}
	effortTexts = []string{
		"short",
		"a longer description of a task that needs considerably more reasoning effort to finish correctly and thoroughly",
	}
	routeLevels = []protocol.Effort{protocol.EffortLow, protocol.EffortHigh}
)

func randomPlan(rng *rand.Rand) protocol.RoutePlanParams {
	roleCount := 3 + rng.Intn(10)
	roles := []protocol.RouteRole{
		routeRoles[0], routeRoles[1],
		{Slot: "conformance:docs-writer", RoleID: "conformance:docs-writer", Name: "Docs writer", Description: "Writes documentation", Kind: protocol.RoleKindCode},
	}
	for len(roles) < roleCount {
		id := fmt.Sprintf("conformance:reviewer-%d", len(roles))
		roles = append(roles, protocol.RouteRole{Slot: id, RoleID: id, Name: "Reviewer", Kind: protocol.RoleKindCheck})
	}
	types := make([]protocol.RouteType, 1+rng.Intn(8))
	states := []protocol.SlotState{protocol.SlotStateOn, protocol.SlotStateOff, protocol.SlotStateOptional}
	for i := range types {
		types[i] = protocol.RouteType{TypeID: fmt.Sprintf("conformance:type-%d", i), Name: fmt.Sprintf("Type %d", i), Roles: map[string]protocol.SlotState{}}
		for _, role := range roles {
			state := states[rng.Intn(len(states))]
			if role.Slot == protocol.SlotSecurity && rng.Intn(2) == 0 {
				state = protocol.SlotStateAuto
			}
			types[i].Roles[role.Slot] = state
		}
	}
	return protocol.RoutePlanParams{Task: routeTasks[rng.Intn(len(routeTasks))], Types: types, Roles: roles}
}

func randomMessage(rng *rand.Rand) protocol.RouteMessageParams {
	return protocol.RouteMessageParams{
		Task:          routeTasks[rng.Intn(len(routeTasks))],
		Message:       routeMessages[rng.Intn(len(routeMessages))],
		CurrentRoleID: routeCandidates[rng.Intn(len(routeCandidates))].RoleID,
		Candidates:    routeCandidates,
	}
}

func randomEffort(rng *rand.Rand) protocol.RouteEffortParams {
	return protocol.RouteEffortParams{
		Text:   effortTexts[rng.Intn(len(effortTexts))],
		RoleID: protocol.RoleDeveloper,
		Kind:   protocol.RoleKindCode,
		Levels: routeLevels,
	}
}
