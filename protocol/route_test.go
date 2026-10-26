// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
)

func planRequest() RoutePlanParams {
	roles := make([]RouteRole, 0, 8)
	for _, s := range Order() {
		kind := RoleKindCode
		switch s {
		case SlotLeadIntake:
			kind = RoleKindIntake
		case SlotLeadAccept:
			kind = RoleKindAccept
		case SlotReviewer, SlotSecurity, SlotTester:
			kind = RoleKindCheck
		}
		roles = append(roles, RouteRole{Slot: s, RoleID: s, Name: s, Kind: kind})
	}
	roles = append(roles, RouteRole{Slot: "x:y", RoleID: "x:y", Name: "Plugin role", Kind: RoleKindRead})
	return RoutePlanParams{
		Task: RouteTask{Title: "t"},
		Types: []RouteType{
			{TypeID: "feature", Name: "Feature", Roles: map[string]SlotState{
				SlotLeadIntake: SlotStateOn, SlotDeveloper: SlotStateOn, SlotSecurity: SlotStateAuto, SlotLeadAccept: SlotStateOn}},
			{TypeID: "bug", Name: "Bug", Roles: map[string]SlotState{
				SlotDeveloper: SlotStateOn, SlotSecurity: SlotStateAuto, SlotLeadAccept: SlotStateOn}},
			{TypeID: "analysis", Name: "Analysis", Roles: map[string]SlotState{
				SlotLeadIntake: SlotStateOn, SlotArchitect: SlotStateOptional}},
		},
		Roles: roles,
	}
}

func issueKinds(t *testing.T, err error) []IssueKind {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	var out []IssueKind
	for _, i := range ve.Issues {
		out = append(out, i.Kind)
	}
	return out
}

func TestRoutePlanResultValidate(t *testing.T) {
	req := planRequest()
	good := RoutePlanResult{
		Type:  &TypeDecision{TypeID: "bug", Confidence: 0.8},
		Roles: []RoleDecision{{Slot: SlotLeadIntake, Confidence: 0.9}, {Slot: SlotTester, Include: true, Confidence: 1}, {Slot: "x:y", Include: true, Confidence: 0.5}},
	}
	if err := good.Validate(req); err != nil {
		t.Fatal(err)
	}
	if err := (RoutePlanResult{}).Validate(req); err != nil {
		t.Fatalf("nil type: %v", err)
	}
	cases := []struct {
		name string
		res  RoutePlanResult
		want IssueKind
	}{
		{"unknown slot", RoutePlanResult{Roles: []RoleDecision{{Slot: "zz"}}}, IssueUnknownID},
		{"unknown type", RoutePlanResult{Type: &TypeDecision{TypeID: "nope", Confidence: 0.5}}, IssueUnknownID},
		{"duplicate", RoutePlanResult{Roles: []RoleDecision{{Slot: SlotTester}, {Slot: SlotTester}}}, IssueDuplicate},
		{"conf high", RoutePlanResult{Roles: []RoleDecision{{Slot: SlotTester, Confidence: 1.01}}}, IssueConfidenceRange},
		{"conf low", RoutePlanResult{Roles: []RoleDecision{{Slot: SlotTester, Confidence: -0.1}}}, IssueConfidenceRange},
		{"conf nan", RoutePlanResult{Roles: []RoleDecision{{Slot: SlotTester, Confidence: math.NaN()}}}, IssueNotFinite},
		{"type conf", RoutePlanResult{Type: &TypeDecision{TypeID: "bug", Confidence: 2}}, IssueConfidenceRange},
	}
	for _, c := range cases {
		err := c.res.Validate(req)
		if !slices.Contains(issueKinds(t, err), c.want) {
			t.Errorf("%s: kinds %v, want %s", c.name, issueKinds(t, err), c.want)
		}
		var ve *ValidationError
		errors.As(err, &ve)
		if !ve.Has(c.want) || ve.Has("other") {
			t.Errorf("%s: Has misbehaves", c.name)
		}
	}
}

func TestRoutePlanResultValidate_AutoAndLeadAcceptAreNotViolations(t *testing.T) {
	res := RoutePlanResult{
		Type:  &TypeDecision{TypeID: "bug", Confidence: 0.8},
		Roles: []RoleDecision{{Slot: SlotSecurity, Include: true, Confidence: 0.9}, {Slot: SlotLeadAccept, Confidence: 0.9}},
	}
	if err := res.Validate(planRequest()); err != nil {
		t.Fatal(err)
	}
}

func TestSanitize(t *testing.T) {
	req := planRequest()
	allLock := planRequest()
	for i := range allLock.Types {
		allLock.Types[i].Roles = map[string]SlotState{SlotLeadAccept: SlotStateOn}
	}
	noTypes := planRequest()
	noTypes.Types = nil
	bug := &TypeDecision{TypeID: "bug", Confidence: 0.9}
	tests := []struct {
		name string
		req  RoutePlanParams
		typ  *TypeDecision
		slot string
		drop bool
	}{
		{"bug security", req, bug, SlotSecurity, true},
		{"bug lead_accept", req, bug, SlotLeadAccept, true},
		{"analysis lead_accept kept", req, &TypeDecision{TypeID: "analysis"}, SlotLeadAccept, false},
		{"feature tester kept", req, &TypeDecision{TypeID: "feature"}, SlotTester, false},
		{"nil type, security locked in feature and bug only", req, nil, SlotSecurity, false},
		{"nil type, lead_accept missing in analysis", req, nil, SlotLeadAccept, false},
		{"nil type, every type locks lead_accept", allLock, nil, SlotLeadAccept, true},
		{"nil type, no types", noTypes, nil, SlotSecurity, false},
		{"plugin slot kept", req, bug, "x:y", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := RoutePlanResult{Type: tc.typ, Roles: []RoleDecision{{Slot: tc.slot, Include: true, Confidence: 0.7}}}
			got, issues := res.Sanitize(tc.req)
			if tc.drop {
				if len(got.Roles) != 0 || len(issues) != 1 || issues[0].Kind != IssueIgnored || issues[0].Path != "roles[0].slot" {
					t.Fatalf("got %+v %+v", got, issues)
				}
				if got.Roles == nil {
					t.Fatal("Roles must be non-nil")
				}
			} else if len(got.Roles) != 1 || len(issues) != 0 {
				t.Fatalf("got %+v %+v", got, issues)
			}
			if len(res.Roles) != 1 {
				t.Fatal("receiver was modified")
			}
		})
	}
	t.Run("order and paths", func(t *testing.T) {
		res := RoutePlanResult{Type: bug, Roles: []RoleDecision{
			{Slot: SlotTester}, {Slot: SlotSecurity}, {Slot: SlotDeveloper}, {Slot: SlotLeadAccept}}}
		got, issues := res.Sanitize(req)
		if len(got.Roles) != 2 || got.Roles[0].Slot != SlotTester || got.Roles[1].Slot != SlotDeveloper {
			t.Fatalf("kept %+v", got.Roles)
		}
		if len(issues) != 2 || issues[0].Path != "roles[1].slot" || issues[1].Path != "roles[3].slot" {
			t.Fatalf("issues %+v", issues)
		}
	})
}

func TestRouteMessageResultValidate(t *testing.T) {
	req := RouteMessageParams{Candidates: []RouteCandidate{{RoleID: "developer"}, {RoleID: "reviewer"}}}
	if err := (RouteMessageResult{RoleID: "developer", Confidence: 0.82, Probabilities: map[string]float64{"developer": 0.82, "reviewer": 0.18}}).Validate(req); err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]RouteMessageResult{
		"unknown role": {RoleID: "x", Confidence: 0.5},
		"prob key":     {RoleID: "developer", Probabilities: map[string]float64{"x": 0.1}},
		"prob value":   {RoleID: "developer", Probabilities: map[string]float64{"developer": 2}},
		"confidence":   {RoleID: "developer", Confidence: -1},
	} {
		if r.Validate(req) == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestRouteEffortResultValidate(t *testing.T) {
	req := RouteEffortParams{Levels: []Effort{EffortLow, EffortMedium, EffortHigh}}
	if err := (RouteEffortResult{Effort: EffortMedium, Confidence: 0.7}).Validate(req); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(issueKinds(t, (RouteEffortResult{Effort: EffortMax, Confidence: 0.7}).Validate(req)), IssueNotInLevels) {
		t.Error("max not in levels")
	}
	if (RouteEffortResult{Effort: EffortLow, Confidence: 3}).Validate(req) == nil {
		t.Error("confidence 3")
	}
}

func TestRouteExplainResultValidate(t *testing.T) {
	if err := (RouteExplainResult{Lines: []ExplainLine{{Text: "a", Value: 0.8}}}).Validate(RoutePlanParams{}); err != nil {
		t.Fatal(err)
	}
	k := issueKinds(t, (RouteExplainResult{Lines: []ExplainLine{{Text: "", Value: math.NaN()}}}).Validate(RoutePlanParams{}))
	if !slices.Contains(k, IssueEmpty) || !slices.Contains(k, IssueNotFinite) {
		t.Errorf("kinds %v", k)
	}
}

func TestValidationErrorTruncates(t *testing.T) {
	var res RoutePlanResult
	for i := 0; i < 8; i++ {
		res.Roles = append(res.Roles, RoleDecision{Slot: fmt.Sprintf("zz%d", i)})
	}
	msg := res.Validate(planRequest()).Error()
	if !strings.Contains(msg, "and 3 more") {
		t.Fatalf("got %q", msg)
	}
}

func TestRouteJSON(t *testing.T) {
	b, _ := json.Marshal(RoutePlanResult{})
	if string(b) != `{"type":null,"roles":[]}` {
		t.Fatalf("got %s", b)
	}
	b, _ = json.Marshal(RouteExplainResult{})
	if string(b) != `{"lines":[]}` {
		t.Fatalf("got %s", b)
	}
}

func TestRoutePlanParamsValidate(t *testing.T) {
	if err := planRequest().Validate(); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("я", 301)
	exact := strings.Repeat("я", 300)
	r := planRequest()
	r.Roles[0].Description = exact
	if err := r.Validate(); err != nil {
		t.Fatalf("300 runes should pass: %v", err)
	}
	mutations := map[string]func(*RoutePlanParams){
		"dup type":        func(p *RoutePlanParams) { p.Types[1].TypeID = "feature" },
		"empty type":      func(p *RoutePlanParams) { p.Types[0].TypeID = "" },
		"dup slot":        func(p *RoutePlanParams) { p.Roles[1].Slot = p.Roles[0].Slot },
		"empty slot":      func(p *RoutePlanParams) { p.Roles[0].Slot = "" },
		"empty roleId":    func(p *RoutePlanParams) { p.Roles[0].RoleID = "" },
		"bad kind":        func(p *RoutePlanParams) { p.Roles[0].Kind = "x" },
		"bad state":       func(p *RoutePlanParams) { p.Types[0].Roles[SlotDeveloper] = "maybe" },
		"empty state key": func(p *RoutePlanParams) { p.Types[0].Roles[""] = SlotStateOn },
		"long role desc":  func(p *RoutePlanParams) { p.Roles[0].Description = long },
		"long type desc":  func(p *RoutePlanParams) { p.Types[0].Description = long },
	}
	for name, mut := range mutations {
		p := planRequest()
		mut(&p)
		if p.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
