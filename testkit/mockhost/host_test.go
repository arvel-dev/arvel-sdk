// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

func TestInProcess_InitializeAndShutdown(t *testing.T) {
	p := streamingProvider()
	p.features.LoadSession = true
	h := InProcess(t, p)

	meta, err := h.Initialize(protocol.InitMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Has(protocol.FeatureStatus) || !meta.Has(protocol.FeatureResume) {
		t.Fatalf("features = %v", meta.Features)
	}
	h.Shutdown()
	if err := h.RunErr(); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestInProcessRouter_InitializeAndShutdown(t *testing.T) {
	r := &stubRouter{status: protocol.Status{State: protocol.StateReady}}
	h := InProcessRouter(t, r)

	meta, err := h.Initialize(protocol.InitMeta{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{protocol.FeatureRoutePlan, protocol.FeatureRouteMessage, protocol.FeatureRouteEffort, protocol.FeatureRouteExplain} {
		if !meta.Has(f) {
			t.Fatalf("missing feature %q in %v", f, meta.Features)
		}
	}
	h.Shutdown()
	if err := h.RunErr(); err != nil {
		t.Fatalf("RunRouter = %v", err)
	}
}

func TestProviderMethods(t *testing.T) {
	p := streamingProvider()
	h := InProcess(t, p)
	if _, err := h.Initialize(protocol.InitMeta{}); err != nil {
		t.Fatal(err)
	}

	st, err := h.Status()
	if err != nil || st.State != protocol.StateReady {
		t.Fatalf("status = %+v %v", st, err)
	}
	mo, err := h.Models(false)
	if err != nil || mo.Default != "m1" {
		t.Fatalf("models = %+v %v", mo, err)
	}
	u, err := h.Usage()
	if err != nil || u.Status != protocol.UsageStatusOK {
		t.Fatalf("usage = %+v %v", u, err)
	}
	sid, err := h.NewSession(protocol.SessionMeta{})
	if err != nil || sid != "s_1" {
		t.Fatalf("new session = %q %v", sid, err)
	}
	pr, err := h.Prompt(sid, []acp.ContentBlock{acp.TextBlock("hi")})
	if err != nil || pr.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("prompt = %+v %v", pr, err)
	}
	if got := h.Updates(sid); len(got) != 1 {
		t.Fatalf("updates = %d", len(got))
	}
	h.Shutdown()
}

func TestRouterMethods(t *testing.T) {
	r := &stubRouter{
		status: protocol.Status{State: protocol.StateReady},
		planFn: func(p protocol.RoutePlanParams) protocol.RoutePlanResult {
			return protocol.RoutePlanResult{Type: &protocol.TypeDecision{TypeID: p.Types[0].TypeID, Confidence: 0.9}}
		},
		messageFn: func(p protocol.RouteMessageParams) protocol.RouteMessageResult {
			return protocol.RouteMessageResult{RoleID: p.Candidates[0].RoleID, Confidence: 0.9}
		},
		effortFn: func(p protocol.RouteEffortParams) protocol.RouteEffortResult {
			return protocol.RouteEffortResult{Effort: p.Levels[0], Confidence: 0.6}
		},
		explainFn: func(protocol.RoutePlanParams) protocol.RouteExplainResult {
			return protocol.RouteExplainResult{Lines: []protocol.ExplainLine{{Text: "x", Value: 0.9}}}
		},
	}
	h := InProcessRouter(t, r)
	if _, err := h.Initialize(protocol.InitMeta{}); err != nil {
		t.Fatal(err)
	}

	plan := protocol.RoutePlanParams{
		Types: []protocol.RouteType{{TypeID: "feature", Name: "Feature"}},
		Roles: []protocol.RouteRole{{Slot: protocol.SlotDeveloper, RoleID: "developer", Name: "Developer", Kind: protocol.RoleKindCode}},
	}
	pr, err := h.RoutePlan(plan)
	if err != nil || pr.Type == nil || pr.Type.TypeID != "feature" {
		t.Fatalf("plan = %+v %v", pr, err)
	}
	msg := protocol.RouteMessageParams{Candidates: []protocol.RouteCandidate{{RoleID: "developer", Name: "Developer", Kind: protocol.RoleKindCode}}}
	mr, err := h.RouteMessage(msg)
	if err != nil || mr.RoleID != "developer" {
		t.Fatalf("message = %+v %v", mr, err)
	}
	eff := protocol.RouteEffortParams{RoleID: "developer", Kind: protocol.RoleKindCode, Levels: []protocol.Effort{protocol.EffortLow}}
	er, err := h.RouteEffort(eff)
	if err != nil || er.Effort != protocol.EffortLow {
		t.Fatalf("effort = %+v %v", er, err)
	}
	xr, err := h.RouteExplain(plan)
	if err != nil || len(xr.Lines) != 1 {
		t.Fatalf("explain = %+v %v", xr, err)
	}
	h.Shutdown()
}

func TestPermissionAutoAnswer(t *testing.T) {
	p := permissionProvider()
	h := InProcess(t, p)
	if _, err := h.Initialize(protocol.InitMeta{}); err != nil {
		t.Fatal(err)
	}
	sid, err := h.NewSession(protocol.SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	h.armPermission("allow_once")
	pr, err := h.Prompt(sid, []acp.ContentBlock{acp.TextBlock("run")})
	if err != nil || pr.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("prompt = %+v %v", pr, err)
	}
	if h.PermissionsSeen() != 1 {
		t.Fatalf("permissions seen = %d", h.PermissionsSeen())
	}
	h.Shutdown()
}
