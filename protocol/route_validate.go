// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// IssueKind classifies one violation found in a router result.
type IssueKind string

const (
	IssueUnknownID       IssueKind = "unknown_id"
	IssueDuplicate       IssueKind = "duplicate"
	IssueConfidenceRange IssueKind = "confidence_range"
	IssueNotInLevels     IssueKind = "not_in_levels"
	IssueNotFinite       IssueKind = "not_finite"
	IssueEmpty           IssueKind = "empty"
	IssueIgnored         IssueKind = "ignored"
)

// Issue is one violation (or, for IssueIgnored, one ignored decision). Path
// looks like "roles[2].slot".
type Issue struct {
	Kind    IssueKind
	Path    string
	Message string
}

// ValidationError lists every violation of a router result. Decisions the
// host merely ignores (PROTOCOL §14.3) are not violations; see Sanitize.
type ValidationError struct {
	Issues []Issue
}

// Error implements the error interface; it shows at most five issues.
func (e *ValidationError) Error() string {
	var b strings.Builder
	b.WriteString("protocol: invalid router result")
	for i, is := range e.Issues {
		if i == 5 {
			fmt.Fprintf(&b, "; and %d more", len(e.Issues)-5)
			break
		}
		fmt.Fprintf(&b, "; %s: %s", is.Path, is.Message)
	}
	return b.String()
}

// Has reports whether any issue has the given kind.
func (e *ValidationError) Has(kind IssueKind) bool {
	return slices.ContainsFunc(e.Issues, func(i Issue) bool { return i.Kind == kind })
}

type issues []Issue

func (is *issues) add(kind IssueKind, path, msg string) {
	*is = append(*is, Issue{Kind: kind, Path: path, Message: msg})
}

func (is *issues) confidence(path string, v float64) {
	switch {
	case math.IsNaN(v) || math.IsInf(v, 0):
		is.add(IssueNotFinite, path, "confidence is not a finite number")
	case v < 0 || v > 1:
		is.add(IssueConfidenceRange, path, "confidence is outside [0, 1]")
	}
}

func (is issues) err() error {
	if len(is) == 0 {
		return nil
	}
	return &ValidationError{Issues: is}
}

// Validate checks the result against the request (PROTOCOL §14.3): the type id
// exists, decisions name slots of the request once each, and confidences are in
// [0, 1]. Decisions on auto slots and on a type's lead_accept are not
// violations: the host ignores them (see Sanitize).
func (r RoutePlanResult) Validate(req RoutePlanParams) error {
	var is issues
	if r.Type != nil {
		if !slices.ContainsFunc(req.Types, func(t RouteType) bool { return t.TypeID == r.Type.TypeID }) {
			is.add(IssueUnknownID, "type.typeId", "type id is not in the request")
		}
		is.confidence("type.confidence", r.Type.Confidence)
	}
	seen := map[string]bool{}
	for i, d := range r.Roles {
		path := fmt.Sprintf("roles[%d]", i)
		if !slices.ContainsFunc(req.Roles, func(ro RouteRole) bool { return ro.Slot == d.Slot }) {
			is.add(IssueUnknownID, path+".slot", "slot is not in the request")
		}
		if seen[d.Slot] {
			is.add(IssueDuplicate, path+".slot", "slot appears twice")
		}
		seen[d.Slot] = true
		is.confidence(path+".confidence", d.Confidence)
	}
	return is.err()
}

// locked reports whether the type decides the slot itself (PROTOCOL §14.3).
func locked(t RouteType, slot string) bool {
	st := t.Roles[slot]
	return st == SlotStateAuto || (slot == SlotLeadAccept && st == SlotStateOn)
}

func ignoredSlot(req RoutePlanParams, d *TypeDecision, slot string) bool {
	if d != nil { // the router named the type: judge against it
		i := slices.IndexFunc(req.Types, func(t RouteType) bool { return t.TypeID == d.TypeID })
		return i >= 0 && locked(req.Types[i], slot)
	}
	// No type named: the user's type is unknown, so ignore the decision only if
	// every offered type locks the slot.
	return len(req.Types) > 0 && !slices.ContainsFunc(req.Types, func(t RouteType) bool { return !locked(t, slot) })
}

// Sanitize returns a copy of the result without the decisions the host ignores
// (PROTOCOL §14.3: auto slots and the lead_accept of a type that has it), and
// one IssueIgnored per dropped decision. It does not check ids or confidences;
// call Validate first. Neither the result nor the request is modified.
func (r RoutePlanResult) Sanitize(req RoutePlanParams) (RoutePlanResult, []Issue) {
	out := RoutePlanResult{Roles: []RoleDecision{}}
	if r.Type != nil {
		t := *r.Type
		out.Type = &t
	}
	var dropped []Issue
	for i, d := range r.Roles {
		if ignoredSlot(req, r.Type, d.Slot) {
			dropped = append(dropped, Issue{Kind: IssueIgnored, Path: fmt.Sprintf("roles[%d].slot", i),
				Message: "decision on an auto or lead_accept slot is ignored"})
			continue
		}
		out.Roles = append(out.Roles, d)
	}
	return out, dropped
}

// Validate checks the result against the request (PROTOCOL §14.4): the role
// and every probability key are candidates; confidences are in [0, 1].
func (r RouteMessageResult) Validate(req RouteMessageParams) error {
	var is issues
	isCandidate := func(id string) bool {
		return slices.ContainsFunc(req.Candidates, func(c RouteCandidate) bool { return c.RoleID == id })
	}
	if !isCandidate(r.RoleID) {
		is.add(IssueUnknownID, "roleId", "role id is not a candidate")
	}
	is.confidence("confidence", r.Confidence)
	for _, id := range slices.Sorted(mapKeys(r.Probabilities)) {
		path := "probabilities[" + id + "]"
		if !isCandidate(id) {
			is.add(IssueUnknownID, path, "role id is not a candidate")
		}
		is.confidence(path, r.Probabilities[id])
	}
	return is.err()
}

func mapKeys(m map[string]float64) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// Validate checks the result against the request (PROTOCOL §14.5): the effort
// is one of the offered levels; confidence is in [0, 1].
func (r RouteEffortResult) Validate(req RouteEffortParams) error {
	var is issues
	if !slices.Contains(req.Levels, r.Effort) {
		is.add(IssueNotInLevels, "effort", "effort is not one of the levels")
	}
	is.confidence("confidence", r.Confidence)
	return is.err()
}

// Validate checks that every line has text and a finite value (PROTOCOL §14.8).
// The request is accepted for symmetry with the other results.
func (r RouteExplainResult) Validate(_ RoutePlanParams) error {
	var is issues
	for i, l := range r.Lines {
		path := fmt.Sprintf("lines[%d]", i)
		if l.Text == "" {
			is.add(IssueEmpty, path+".text", "text is empty")
		}
		if math.IsNaN(l.Value) || math.IsInf(l.Value, 0) {
			is.add(IssueNotFinite, path+".value", "value is not a finite number")
		}
	}
	return is.err()
}
