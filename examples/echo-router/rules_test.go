// SPDX-License-Identifier: MIT-0

package main

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

func roster() protocol.RoutePlanParams {
	return protocol.RoutePlanParams{
		Types: []protocol.RouteType{
			{TypeID: "feature", Name: "Feature", Roles: map[string]protocol.SlotState{
				protocol.SlotDeveloper: protocol.SlotStateOn, protocol.SlotSecurity: protocol.SlotStateAuto,
				protocol.SlotTester: protocol.SlotStateOff, protocol.SlotLeadAccept: protocol.SlotStateOn}},
			{TypeID: "bug", Name: "Bug", Roles: map[string]protocol.SlotState{
				protocol.SlotDeveloper: protocol.SlotStateOn, protocol.SlotTester: protocol.SlotStateOptional}},
		},
		Roles: []protocol.RouteRole{
			{Slot: protocol.SlotDeveloper, RoleID: "developer", Name: "Developer", Description: "Writes code by the plan", Kind: protocol.RoleKindCode},
			{Slot: protocol.SlotSecurity, RoleID: "security", Name: "Security", Description: "Checks authentication", Kind: protocol.RoleKindCheck},
			{Slot: protocol.SlotTester, RoleID: "tester", Name: "Tester", Description: "Runs tests and browser checks", Kind: protocol.RoleKindCheck},
			{Slot: protocol.SlotLeadAccept, RoleID: "lead", Name: "Lead", Description: "Accepts the result", Kind: protocol.RoleKindAccept},
		},
	}
}

func plan(title string, mutate ...func(*protocol.RoutePlanParams)) protocol.RoutePlanResult {
	p := roster()
	p.Task.Title = title
	for _, m := range mutate {
		m(&p)
	}
	res, _ := planDecision(p)
	return res
}

func TestPlan_TypeNameWins(t *testing.T) {
	res := plan("Add a feature flag")
	if res.Type == nil || res.Type.TypeID != "feature" || res.Type.Confidence != 0.9 {
		t.Fatalf("type = %+v", res.Type)
	}
	if res := plan("Bug in feature code"); res.Type == nil || res.Type.TypeID != "feature" {
		t.Fatalf("the longest name wins: %+v", res.Type)
	}
}

func TestPlan_KeywordsSuggestBug(t *testing.T) {
	for _, title := range []string{"Тест падает после коммита", "Build fails", "ошибка при оплате", "there is a BUG"} {
		if title == "there is a BUG" { // the name "Bug" matches first: still the bug type
			res := plan(title)
			if res.Type == nil || res.Type.TypeID != "bug" || res.Type.Confidence != 0.9 {
				t.Errorf("%q: %+v", title, res.Type)
			}
			continue
		}
		res := plan(title)
		if res.Type == nil || res.Type.TypeID != "bug" || res.Type.Confidence != 0.8 {
			t.Errorf("%q: %+v", title, res.Type)
		}
	}
	noBug := func(p *protocol.RoutePlanParams) { p.Types = p.Types[:1] }
	if res := plan("Build fails", noBug); res.Type != nil {
		t.Fatalf("no bug type offered: %+v", res.Type)
	}
}

func TestPlan_NoMatchHasNilTypeAndEmptyRoles(t *testing.T) {
	res := plan("Something unrelated")
	if res.Type != nil || res.Roles == nil || len(res.Roles) != 0 {
		t.Fatalf("%+v", res)
	}
}

func decisionFor(res protocol.RoutePlanResult, slot string) *protocol.RoleDecision {
	for i := range res.Roles {
		if res.Roles[i].Slot == slot {
			return &res.Roles[i]
		}
	}
	return nil
}

func TestPlan_RoleIncludedByNameOrDescriptionWord(t *testing.T) {
	// feature type: tester is off.
	if d := decisionFor(plan("Add a feature, please run the browser"), protocol.SlotTester); d == nil || !d.Include || d.Confidence != 0.9 {
		t.Fatalf("browser: %+v", d)
	}
	if d := decisionFor(plan("Add a feature, testing only"), protocol.SlotTester); d != nil {
		t.Fatalf("testing is not a description word: %+v", d)
	}
	if d := decisionFor(plan("Add a feature, ask the tester"), protocol.SlotTester); d == nil || !d.Include {
		t.Fatalf("name: %+v", d)
	}
	// bug type: tester is optional.
	if d := decisionFor(plan("Bug: browser crashes"), protocol.SlotTester); d == nil || !d.Include {
		t.Fatalf("optional role: %+v", d)
	}
}

func TestPlan_RoleExcludedByWithout(t *testing.T) {
	for _, title := range []string{"Add a feature without developer", "Add a feature без developer"} {
		if d := decisionFor(plan(title), protocol.SlotDeveloper); d == nil || d.Include {
			t.Errorf("%q: %+v", title, d)
		}
	}
	if d := decisionFor(plan("Add a feature, the developer knows"), protocol.SlotDeveloper); d != nil {
		t.Fatalf("name alone must not exclude: %+v", d)
	}
	if d := decisionFor(plan("Add a feature with security and lead authentication"), protocol.SlotSecurity); d != nil {
		t.Fatalf("auto slots are never decided: %+v", d)
	}
	if d := decisionFor(plan("Add a feature, ask the lead to accept the result"), protocol.SlotLeadAccept); d != nil {
		t.Fatalf("lead_accept is never decided: %+v", d)
	}
}

func TestPlan_ReferenceTypeFallsBackToFirstType(t *testing.T) {
	// No type decided: the first type (feature) says tester is off, so a hit includes it.
	res := plan("Please run the browser")
	if res.Type != nil {
		t.Fatalf("type = %+v", res.Type)
	}
	if d := decisionFor(res, protocol.SlotTester); d == nil || !d.Include {
		t.Fatalf("%+v", d)
	}
	// No types at all: every slot counts as off.
	res = plan("Please run the browser", func(p *protocol.RoutePlanParams) { p.Types = nil })
	if d := decisionFor(res, protocol.SlotTester); d == nil || !d.Include {
		t.Fatalf("%+v", d)
	}
}

func TestPlan_ResultValidatesAgainstRequest(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	words := strings.Fields("feature bug fails browser without developer tester security lead ошибка plan code review")
	kinds := protocol.AllRoleKinds()
	states := protocol.AllSlotStates()
	for i := range 200 {
		var p protocol.RoutePlanParams
		order := protocol.Order()
		for j := range 1 + rng.Intn(10) {
			slot := protocol.PluginSlot("p", words[j%len(words)]+string(rune('a'+j)))
			if j < len(order) {
				slot = order[j]
			}
			p.Roles = append(p.Roles, protocol.RouteRole{Slot: slot, RoleID: slot, Name: words[rng.Intn(len(words))],
				Description: strings.Join(words[:rng.Intn(len(words))], " "), Kind: kinds[rng.Intn(len(kinds))]})
		}
		for j := range rng.Intn(4) {
			t := protocol.RouteType{TypeID: []string{"feature", "bug", "other", "x"}[j], Name: words[rng.Intn(len(words))], Roles: map[string]protocol.SlotState{}}
			for _, r := range p.Roles {
				t.Roles[r.Slot] = states[rng.Intn(len(states))]
			}
			p.Types = append(p.Types, t)
		}
		var title []string
		for range 1 + rng.Intn(6) {
			title = append(title, words[rng.Intn(len(words))])
		}
		p.Task.Title = strings.Join(title, " ")
		res, lines := planDecision(p)
		if err := res.Validate(p); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if _, ignored := res.Sanitize(p); len(ignored) != 0 {
			t.Fatalf("request %d: decided an ignored slot: %v", i, ignored)
		}
		want := len(res.Roles)
		if res.Type != nil {
			want++
		}
		if len(lines) != want {
			t.Fatalf("request %d: %d lines for %d decisions", i, len(lines), want)
		}
	}
}

func TestMessage(t *testing.T) {
	cands := []protocol.RouteCandidate{
		{RoleID: "developer", Name: "Developer"}, {RoleID: "dev-ops", Name: "Developer Ops"}, {RoleID: "reviewer", Name: "Ревьюер"},
	}
	cases := []struct {
		name, msg, current, want string
		conf                     float64
	}{
		{"name hit", "ask the developer", "reviewer", "developer", 0.9},
		{"case-insensitive cyrillic", "Пусть РЕВЬЮЕР посмотрит", "developer", "reviewer", 0.9},
		{"longest name wins", "call developer ops now", "", "dev-ops", 0.9},
		{"current role", "no names here", "reviewer", "reviewer", 0},
		{"first candidate", "no names here", "ghost", "developer", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := protocol.RouteMessageParams{Message: c.msg, CurrentRoleID: c.current, Candidates: cands}
			res, err := messageDecision(p)
			if err != nil || res.RoleID != c.want || res.Confidence != c.conf {
				t.Fatalf("%+v %v", res, err)
			}
			if err := res.Validate(p); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := messageDecision(protocol.RouteMessageParams{Message: "x"}); err == nil {
		t.Fatal("no candidates must be an error")
	}
}

func TestEffort(t *testing.T) {
	all := []protocol.Effort{protocol.EffortLow, protocol.EffortMedium, protocol.EffortHigh}
	for _, c := range []struct {
		n      int
		levels []protocol.Effort
		want   protocol.Effort
	}{
		{79, all, protocol.EffortLow}, {80, all, protocol.EffortMedium}, {600, all, protocol.EffortMedium}, {601, all, protocol.EffortHigh},
		{10, []protocol.Effort{protocol.EffortMedium, protocol.EffortHigh}, protocol.EffortMedium},
		{700, []protocol.Effort{protocol.EffortLow, protocol.EffortMax}, protocol.EffortLow}, // tie: the lower level
	} {
		p := protocol.RouteEffortParams{Text: strings.Repeat("я", c.n), Levels: c.levels}
		res, err := effortDecision(p)
		if err != nil || res.Effort != c.want || res.Confidence != 0.6 {
			t.Errorf("%d chars: %+v %v, want %s", c.n, res, err, c.want)
		}
		if err := res.Validate(p); err != nil {
			t.Error(err)
		}
	}
	if _, err := effortDecision(protocol.RouteEffortParams{Text: "x"}); err == nil {
		t.Fatal("no levels must be an error")
	}
}

func TestExplain_OneLinePerFiredRule(t *testing.T) {
	p := roster()
	p.Task.Title = "Build fails in the browser"
	_, lines := planDecision(p)
	if len(lines) != 2 { // the bug keyword and the tester (optional in bug)
		t.Fatalf("lines = %+v", lines)
	}
	_, none := planDecision(protocol.RoutePlanParams{Task: protocol.RouteTask{Title: "nothing"}})
	if none == nil || len(none) != 0 {
		t.Fatalf("no rule fired: %+v", none)
	}
}
