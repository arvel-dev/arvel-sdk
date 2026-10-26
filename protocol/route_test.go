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
	return RoutePlanParams{
		Task: RouteTask{Title: "t"},
		Steps: []RouteStep{
			{StepID: "s1", RoleID: "r1", Name: "A", Kind: WorkKindSpec, Participation: ParticipationIfNeeded},
			{StepID: "s2", RoleID: "r2", Name: "B", Kind: WorkKindPlan, Participation: ParticipationUsually},
			{StepID: "s3", RoleID: "r3", Name: "C", Kind: WorkKindCode, Participation: ParticipationAlways},
		},
		Presets: []RoutePreset{{PresetID: "p1", Name: "Bug"}},
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
		Preset: &PresetDecision{PresetID: "p1", Confidence: 0.8},
		Steps:  []StepDecision{{StepID: "s1", Confidence: 0.9}, {StepID: "s2", Include: true, Confidence: 1}},
	}
	if err := good.Validate(req); err != nil {
		t.Fatal(err)
	}
	if err := (RoutePlanResult{}).Validate(req); err != nil {
		t.Fatalf("nil preset: %v", err)
	}
	cases := []struct {
		name string
		res  RoutePlanResult
		want IssueKind
	}{
		{"unknown step", RoutePlanResult{Steps: []StepDecision{{StepID: "zz"}}}, IssueUnknownID},
		{"always step", RoutePlanResult{Steps: []StepDecision{{StepID: "s3"}}}, IssueNotDecidable},
		{"duplicate", RoutePlanResult{Steps: []StepDecision{{StepID: "s1"}, {StepID: "s1"}}}, IssueDuplicate},
		{"conf high", RoutePlanResult{Steps: []StepDecision{{StepID: "s1", Confidence: 1.01}}}, IssueConfidenceRange},
		{"conf low", RoutePlanResult{Steps: []StepDecision{{StepID: "s1", Confidence: -0.1}}}, IssueConfidenceRange},
		{"conf nan", RoutePlanResult{Steps: []StepDecision{{StepID: "s1", Confidence: math.NaN()}}}, IssueNotFinite},
		{"unknown preset", RoutePlanResult{Preset: &PresetDecision{PresetID: "px", Confidence: 0.5}}, IssueUnknownID},
		{"preset conf", RoutePlanResult{Preset: &PresetDecision{PresetID: "p1", Confidence: 2}}, IssueConfidenceRange},
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

func TestRouteMessageResultValidate(t *testing.T) {
	req := RouteMessageParams{Candidates: []RouteCandidate{{RoleID: "r_dev"}, {RoleID: "r_review"}}}
	if err := (RouteMessageResult{RoleID: "r_dev", Confidence: 0.82, Probabilities: map[string]float64{"r_dev": 0.82, "r_review": 0.18}}).Validate(req); err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]RouteMessageResult{
		"unknown role": {RoleID: "x", Confidence: 0.5},
		"prob key":     {RoleID: "r_dev", Probabilities: map[string]float64{"x": 0.1}},
		"prob value":   {RoleID: "r_dev", Probabilities: map[string]float64{"r_dev": 2}},
		"confidence":   {RoleID: "r_dev", Confidence: -1},
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
		res.Steps = append(res.Steps, StepDecision{StepID: fmt.Sprintf("zz%d", i)})
	}
	msg := res.Validate(planRequest()).Error()
	if !strings.Contains(msg, "and 3 more") {
		t.Fatalf("got %q", msg)
	}
}

func TestRouteJSON(t *testing.T) {
	b, _ := json.Marshal(RoutePlanResult{})
	if string(b) != `{"preset":null,"steps":[]}` {
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
	r.Steps[0].Description = exact
	if err := r.Validate(); err != nil {
		t.Fatalf("300 runes should pass: %v", err)
	}
	mutations := map[string]func(*RoutePlanParams){
		"dup step":  func(p *RoutePlanParams) { p.Steps[1].StepID = "s1" },
		"bad kind":  func(p *RoutePlanParams) { p.Steps[0].Kind = "x" },
		"bad part":  func(p *RoutePlanParams) { p.Steps[0].Participation = "x" },
		"long desc": func(p *RoutePlanParams) { p.Steps[0].Description = long },
		"long pre":  func(p *RoutePlanParams) { p.Presets[0].Description = long },
		"dup pre":   func(p *RoutePlanParams) { p.Presets = append(p.Presets, p.Presets[0]) },
	}
	for name, mut := range mutations {
		p := planRequest()
		mut(&p)
		if p.Validate() == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
