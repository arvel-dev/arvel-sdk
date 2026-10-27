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
func checkRouteValidity(ctx context.Context, f *flags) CheckResult {
	d, err := startDriver(f)
	if err != nil {
		return fail("route validity", "start: "+err.Error())
	}
	defer d.Close()
	if _, err := d.Initialize(releaseInitMeta()); err != nil {
		return fail("route validity", "initialize: "+err.Error())
	}
	rng := rand.New(rand.NewSource(1)) // seeded for reproducibility
	return checkRouteValidityRequests(ctx, d, rng)
}

// checkRouteValidityRequests sends randomizedRequests route requests and asserts
// each response is valid per the protocol validators (ids/slots/efforts from
// the request, confidence in [0, 1], finite explain values). A single invalid
// response is collected, not fatal; every request is tried unless the transport
// itself dies.
func checkRouteValidityRequests(ctx context.Context, d *mockhost.Driver, rng *rand.Rand) CheckResult {
	var violations []string
	for i := 0; i < randomizedRequests; i++ {
		name, err := oneRandomRoute(ctx, d, rng, i)
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
func oneRandomRoute(ctx context.Context, d *mockhost.Driver, rng *rand.Rand, i int) (string, error) {
	switch i % 4 {
	case 0:
		req := randomPlan(rng)
		var res protocol.RoutePlanResult
		if err := callRoute(ctx, d, protocol.MethodRoutePlan, req, &res); err != nil {
			return "route/plan", err
		}
		return "route/plan", res.Validate(req)
	case 1:
		req := randomMessage(rng)
		var res protocol.RouteMessageResult
		if err := callRoute(ctx, d, protocol.MethodRouteMessage, req, &res); err != nil {
			return "route/message", err
		}
		return "route/message", res.Validate(req)
	case 2:
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
	routeTypes = []protocol.RouteType{
		{TypeID: protocol.TypeFeature, Name: "Feature", Roles: map[string]protocol.SlotState{
			protocol.SlotDeveloper: protocol.SlotStateOn, protocol.SlotSecurity: protocol.SlotStateAuto,
			protocol.SlotTester: protocol.SlotStateOff, protocol.SlotLeadAccept: protocol.SlotStateOn,
		}},
		{TypeID: protocol.TypeBug, Name: "Bug", Roles: map[string]protocol.SlotState{
			protocol.SlotDeveloper: protocol.SlotStateOn, protocol.SlotTester: protocol.SlotStateOptional,
		}},
	}
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
	return protocol.RoutePlanParams{
		Task:  routeTasks[rng.Intn(len(routeTasks))],
		Types: routeTypes,
		Roles: routeRoles,
	}
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
