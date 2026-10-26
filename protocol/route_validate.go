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
	IssueNotDecidable    IssueKind = "not_decidable"
	IssueDuplicate       IssueKind = "duplicate"
	IssueConfidenceRange IssueKind = "confidence_range"
	IssueNotInLevels     IssueKind = "not_in_levels"
	IssueNotFinite       IssueKind = "not_finite"
	IssueEmpty           IssueKind = "empty"
)

// Issue is one violation. Path looks like "steps[2].stepId".
type Issue struct {
	Kind    IssueKind
	Path    string
	Message string
}

// ValidationError lists every violation of a router result. A host that is
// lenient about some kinds (PROTOCOL §14.3 lets it ignore unknown and "always"
// steps) can inspect Issues or call Has instead of rejecting the whole result.
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

// Validate checks the result against the request (PROTOCOL §14.3): the preset
// id exists, decisions name only decidable steps of the request once each,
// and confidences are in [0, 1].
func (r RoutePlanResult) Validate(req RoutePlanParams) error {
	var is issues
	if r.Preset != nil {
		if !slices.ContainsFunc(req.Presets, func(p RoutePreset) bool { return p.PresetID == r.Preset.PresetID }) {
			is.add(IssueUnknownID, "preset.presetId", "preset id is not in the request")
		}
		is.confidence("preset.confidence", r.Preset.Confidence)
	}
	seen := map[string]bool{}
	for i, d := range r.Steps {
		path := fmt.Sprintf("steps[%d]", i)
		idx := slices.IndexFunc(req.Steps, func(s RouteStep) bool { return s.StepID == d.StepID })
		switch {
		case idx < 0:
			is.add(IssueUnknownID, path+".stepId", "step id is not in the request")
		case !req.Steps[idx].Participation.Decidable():
			is.add(IssueNotDecidable, path+".stepId", "step is not decidable")
		}
		if seen[d.StepID] {
			is.add(IssueDuplicate, path+".stepId", "step id appears twice")
		}
		seen[d.StepID] = true
		is.confidence(path+".confidence", d.Confidence)
	}
	return is.err()
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
