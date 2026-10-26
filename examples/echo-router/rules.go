// SPDX-License-Identifier: MIT-0

package main

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

const (
	confMatch  = 0.9
	confBug    = 0.8
	confEffort = 0.6
)

// bugKeywords suggest the bug type, checked in this order.
var bugKeywords = []string{"ошибка", "падает", "bug", "fails"}

func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// tokens returns the set of lower-case runs of letters in s.
func tokens(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) }) {
		set[w] = true
	}
	return set
}

// descriptionWords returns the words of s longer than five letters.
func descriptionWords(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if utf8.RuneCountInString(w) > 5 {
			out = append(out, w)
		}
	}
	return out
}

// planDecision applies the rules of route.plan and says which rule fired.
func planDecision(p protocol.RoutePlanParams) (protocol.RoutePlanResult, []protocol.ExplainLine) {
	text := fold(p.Task.Title + " " + p.Task.Description)
	words := tokens(text)
	lines := make([]protocol.ExplainLine, 0)
	res := protocol.RoutePlanResult{Roles: []protocol.RoleDecision{}}

	// Type: the longest type name that appears in the text, else the bug keywords.
	best := -1
	for i, t := range p.Types {
		name := fold(t.Name)
		if name == "" || !strings.Contains(text, name) {
			continue
		}
		if best < 0 || utf8.RuneCountInString(name) > utf8.RuneCountInString(fold(p.Types[best].Name)) {
			best = i
		}
	}
	switch {
	case best >= 0:
		res.Type = &protocol.TypeDecision{TypeID: p.Types[best].TypeID, Confidence: confMatch}
		lines = append(lines, protocol.ExplainLine{Text: fmt.Sprintf("type name %q appears in the text", p.Types[best].Name), Value: confMatch})
	default:
		hasBug := slices.ContainsFunc(p.Types, func(t protocol.RouteType) bool { return t.TypeID == protocol.TypeBug })
		for _, kw := range bugKeywords {
			if hasBug && strings.Contains(text, kw) {
				res.Type = &protocol.TypeDecision{TypeID: protocol.TypeBug, Confidence: confBug}
				lines = append(lines, protocol.ExplainLine{Text: fmt.Sprintf("keyword %q suggests the bug type", kw), Value: confBug})
				break
			}
		}
	}

	// Roles are judged against the decided type, else the first offered type.
	var ref *protocol.RouteType
	if res.Type != nil {
		if i := slices.IndexFunc(p.Types, func(t protocol.RouteType) bool { return t.TypeID == res.Type.TypeID }); i >= 0 {
			ref = &p.Types[i]
		}
	} else if len(p.Types) > 0 {
		ref = &p.Types[0]
	}
	for _, r := range p.Roles {
		if r.Slot == protocol.SlotLeadAccept {
			continue
		}
		state := protocol.SlotStateOff
		if ref != nil {
			if st, ok := ref.Roles[r.Slot]; ok {
				state = st
			}
		}
		name := fold(r.Name)
		switch state {
		case protocol.SlotStateAuto:
			continue
		case protocol.SlotStateOn:
			for _, pre := range []string{"без ", "without "} {
				if name != "" && strings.Contains(text, pre+name) {
					res.Roles = append(res.Roles, protocol.RoleDecision{Slot: r.Slot, Include: false, Confidence: confMatch})
					lines = append(lines, protocol.ExplainLine{Text: fmt.Sprintf("role %q is excluded by %q", r.Name, pre+name), Value: confMatch})
					break
				}
			}
		default: // off, optional
			hit := ""
			if name != "" && strings.Contains(text, name) {
				hit = name
			} else {
				for _, w := range descriptionWords(r.Description) {
					if words[w] {
						hit = w
						break
					}
				}
			}
			if hit != "" {
				res.Roles = append(res.Roles, protocol.RoleDecision{Slot: r.Slot, Include: true, Confidence: confMatch})
				lines = append(lines, protocol.ExplainLine{Text: fmt.Sprintf("role %q matches %q", r.Name, hit), Value: confMatch})
			}
		}
	}
	return res, lines
}

// messageDecision picks the candidate whose name appears in the message.
func messageDecision(p protocol.RouteMessageParams) (protocol.RouteMessageResult, error) {
	if len(p.Candidates) == 0 {
		return protocol.RouteMessageResult{}, protocol.NewError(protocol.ErrProviderError, "no candidates", map[string]any{"vendorCode": "no_candidates"})
	}
	msg := fold(p.Message)
	best := -1
	for i, c := range p.Candidates {
		name := fold(c.Name)
		if name == "" || !strings.Contains(msg, name) {
			continue
		}
		if best < 0 || utf8.RuneCountInString(name) > utf8.RuneCountInString(fold(p.Candidates[best].Name)) {
			best = i
		}
	}
	if best >= 0 {
		return protocol.RouteMessageResult{RoleID: p.Candidates[best].RoleID, Confidence: confMatch}, nil
	}
	if slices.ContainsFunc(p.Candidates, func(c protocol.RouteCandidate) bool { return c.RoleID == p.CurrentRoleID }) {
		return protocol.RouteMessageResult{RoleID: p.CurrentRoleID}, nil
	}
	return protocol.RouteMessageResult{RoleID: p.Candidates[0].RoleID}, nil
}

// effortDecision maps the text length to an effort the role's model offers.
func effortDecision(p protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
	if len(p.Levels) == 0 {
		return protocol.RouteEffortResult{}, protocol.NewError(protocol.ErrProviderError, "no levels", map[string]any{"vendorCode": "no_levels"})
	}
	wanted := protocol.EffortMedium
	switch n := utf8.RuneCountInString(p.Text); {
	case n < 80:
		wanted = protocol.EffortLow
	case n > 600:
		wanted = protocol.EffortHigh
	}
	all := protocol.AllEfforts()
	rank := func(e protocol.Effort) int { return slices.Index(all, e) }
	best := p.Levels[0]
	for _, l := range p.Levels[1:] {
		d, bd := abs(rank(l)-rank(wanted)), abs(rank(best)-rank(wanted))
		if d < bd || (d == bd && rank(l) < rank(best)) {
			best = l
		}
	}
	return protocol.RouteEffortResult{Effort: best, Confidence: confEffort}, nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
