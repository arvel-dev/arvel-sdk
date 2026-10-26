// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

func TestE2E_RouterConversation(t *testing.T) {
	r := newStubRouter(t)
	h := newRouterHarness(t, routerAll{r})
	var res json.RawMessage
	if err := h.call("initialize", golden(t, "5.1-initialize-request.json", "/params"), &res); err != nil {
		t.Fatal(err)
	}
	if got := routerFeatures(t, res); len(got) != 4 {
		t.Fatalf("features = %v", got)
	}
	if err := h.call(protocol.MethodStatus, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, req, reqPtr, res, resPtr string }{
		{protocol.MethodRoutePlan, "14.3-route-plan-request.json", "/params", "14.3-route-plan-result.json", "/result"},
		{protocol.MethodRouteMessage, "14.4-route-message-request.json", "/params", "14.4-route-message-result.json", ""},
		{protocol.MethodRouteEffort, "14.5-route-effort-request.json", "/params", "14.5-route-effort-result.json", ""},
		{protocol.MethodRouteExplain, "14.3-route-plan-request.json", "/params", "14.8-route-explain-result.json", ""},
	} {
		var got json.RawMessage
		if err := h.call(tc.method, golden(t, tc.req, tc.reqPtr), &got); err != nil {
			t.Fatalf("%s: %v", tc.method, err)
		}
		mustEqualJSON(t, golden(t, tc.res, tc.resPtr), got)
	}
	if err := h.call(protocol.MethodSessionNew, map[string]any{"cwd": "/x", "mcpServers": []any{}}, nil); rpcCode(err) != protocol.CodeMethodNotFound {
		t.Fatalf("session/new on a router: %v", err)
	}
	h.shutdown()
	if h.runErr != nil || r.shutdownN.Load() != 1 {
		t.Fatalf("Run = %v, shutdown calls = %d", h.runErr, r.shutdownN.Load())
	}
}

// randomRoster builds a plan request with 1-12 steps and 0-3 presets.
func randomRoster(rng *rand.Rand) protocol.RoutePlanParams {
	kinds := protocol.AllWorkKinds()
	parts := protocol.AllParticipations()
	p := protocol.RoutePlanParams{Task: protocol.RouteTask{Title: "task", Language: "en"}}
	for i := range 1 + rng.Intn(12) {
		p.Steps = append(p.Steps, protocol.RouteStep{
			StepID: fmt.Sprintf("s%d", i), RoleID: fmt.Sprintf("r%d", i), Name: fmt.Sprintf("Role %d", i),
			Kind: kinds[rng.Intn(len(kinds))], Participation: parts[rng.Intn(len(parts))],
		})
	}
	for i := range rng.Intn(4) {
		p.Presets = append(p.Presets, protocol.RoutePreset{PresetID: fmt.Sprintf("p%d", i), Name: fmt.Sprintf("Preset %d", i)})
	}
	return p
}

// validDecisions answers a plan request correctly.
func validDecisions(rng *rand.Rand, req protocol.RoutePlanParams) protocol.RoutePlanResult {
	var res protocol.RoutePlanResult
	if len(req.Presets) > 0 && rng.Intn(2) == 0 {
		res.Preset = &protocol.PresetDecision{PresetID: req.Presets[rng.Intn(len(req.Presets))].PresetID, Confidence: rng.Float64()}
	}
	for _, s := range req.Steps {
		if s.Participation.Decidable() {
			res.Steps = append(res.Steps, protocol.StepDecision{StepID: s.StepID, Include: rng.Intn(2) == 0, Confidence: rng.Float64()})
		}
	}
	return res
}

func TestE2E_RouterRandomRosters(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	r := newStubRouter(t)
	var next protocol.RoutePlanResult
	r.plan = func(_ context.Context, req protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
		next = validDecisions(rng, req)
		return next, nil
	}
	h := readyRouter(t, routerAll{r})
	for i := range 100 {
		req := randomRoster(rng)
		var got protocol.RoutePlanResult
		if err := h.call(protocol.MethodRoutePlan, req, &got); err != nil {
			t.Fatalf("roster %d: %v", i, err)
		}
		if err := got.Validate(req); err != nil { // what the host checks
			t.Fatalf("roster %d: invalid result reached the host: %v", i, err)
		}
	}
}

func TestE2E_RouterViolationsNeverReachTheHost(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	violations := map[string]func(res *protocol.RoutePlanResult, req protocol.RoutePlanParams){
		"foreign_step_id": func(res *protocol.RoutePlanResult, _ protocol.RoutePlanParams) {
			res.Steps = append(res.Steps, protocol.StepDecision{StepID: "not-in-request", Confidence: 0.5})
		},
		"confidence_two": func(res *protocol.RoutePlanResult, req protocol.RoutePlanParams) {
			res.Steps = append(res.Steps[:0:0], protocol.StepDecision{StepID: req.Steps[0].StepID, Confidence: 2})
		},
		"foreign_preset": func(res *protocol.RoutePlanResult, _ protocol.RoutePlanParams) {
			res.Preset = &protocol.PresetDecision{PresetID: "not-in-request", Confidence: 0.5}
		},
	}
	for name, violate := range violations {
		t.Run(name, func(t *testing.T) {
			r := newStubRouter(t)
			r.plan = func(_ context.Context, req protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
				res := validDecisions(rng, req)
				violate(&res, req)
				return res, nil
			}
			h := readyRouter(t, routerAll{r})
			for i := range 20 {
				req := randomRoster(rng)
				if len(req.Steps) == 0 {
					continue
				}
				var got json.RawMessage
				err := h.call(protocol.MethodRoutePlan, req, &got)
				if arvelCode(err) != protocol.ErrInternal || got != nil {
					t.Fatalf("roster %d: err = %v, result = %s", i, err, got)
				}
			}
		})
	}
}
