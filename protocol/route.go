// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// MaxRoleDescriptionLen is the longest role description the host sends
// (PROTOCOL §14.2), counted in runes.
const MaxRoleDescriptionLen = 300

// RouteTask is the task text the host sends to a router.
type RouteTask struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Projects    []string `json:"projects,omitempty"`
	Language    string   `json:"language,omitempty"`
}

// RouteStep is one role step of the team order.
type RouteStep struct {
	StepID        string        `json:"stepId"`
	RoleID        string        `json:"roleId"`
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	Kind          WorkKind      `json:"kind"`
	Participation Participation `json:"participation"`
}

// RoutePreset is a named selection of steps.
type RoutePreset struct {
	PresetID    string `json:"presetId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// RoutePlanParams are the params of _arvel/route/plan (and route/explain).
type RoutePlanParams struct {
	Task    RouteTask     `json:"task"`
	Steps   []RouteStep   `json:"steps"`
	Presets []RoutePreset `json:"presets,omitempty"`
}

// PresetDecision is the router's preset choice.
type PresetDecision struct {
	PresetID   string  `json:"presetId"`
	Confidence float64 `json:"confidence"`
}

// StepDecision is the router's decision about one decidable step.
type StepDecision struct {
	StepID     string  `json:"stepId"`
	Include    bool    `json:"include"`
	Confidence float64 `json:"confidence"`
}

// RoutePlanResult is the result of _arvel/route/plan. A nil Preset marshals as
// null ("no preset fits").
type RoutePlanResult struct {
	Preset *PresetDecision `json:"preset"`
	Steps  []StepDecision  `json:"steps"`
}

// MarshalJSON always writes "steps" as an array.
func (r RoutePlanResult) MarshalJSON() ([]byte, error) {
	type plain RoutePlanResult
	p := plain(r)
	if p.Steps == nil {
		p.Steps = []StepDecision{}
	}
	return json.Marshal(p)
}

// RouteCandidate is a role that may answer a message.
type RouteCandidate struct {
	RoleID      string   `json:"roleId"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Kind        WorkKind `json:"kind"`
}

// RouteMessageParams are the params of _arvel/route/message.
type RouteMessageParams struct {
	Task          RouteTask        `json:"task"`
	Message       string           `json:"message"`
	CurrentRoleID string           `json:"currentRoleId,omitempty"`
	Candidates    []RouteCandidate `json:"candidates"`
}

// RouteMessageResult is the result of _arvel/route/message.
type RouteMessageResult struct {
	RoleID        string             `json:"roleId"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// RouteEffortParams are the params of _arvel/route/effort.
type RouteEffortParams struct {
	Text   string   `json:"text"`
	RoleID string   `json:"roleId"`
	Kind   WorkKind `json:"kind"`
	Levels []Effort `json:"levels"`
}

// RouteEffortResult is the result of _arvel/route/effort.
type RouteEffortResult struct {
	Effort     Effort  `json:"effort"`
	Confidence float64 `json:"confidence"`
}

// ExplainLine is one line of a route explanation.
type ExplainLine struct {
	Text  string  `json:"text"`
	Value float64 `json:"value"`
}

// RouteExplainResult is the result of _arvel/route/explain.
type RouteExplainResult struct {
	Lines []ExplainLine `json:"lines"`
}

func checkDescription(errs *[]error, path, desc string) {
	if utf8.RuneCountInString(desc) > MaxRoleDescriptionLen {
		*errs = append(*errs, fmt.Errorf("protocol: %s.description is longer than %d characters", path, MaxRoleDescriptionLen))
	}
}

// Validate checks the request the host builds: unique ids, valid enums and
// description lengths (PROTOCOL §14.2, §14.3).
func (p RoutePlanParams) Validate() error {
	var errs []error
	steps := map[string]bool{}
	for i, s := range p.Steps {
		path := fmt.Sprintf("steps[%d]", i)
		if s.StepID == "" || steps[s.StepID] {
			errs = append(errs, fmt.Errorf("protocol: %s.stepId is empty or duplicated", path))
		}
		steps[s.StepID] = true
		if !s.Kind.Valid() {
			errs = append(errs, fmt.Errorf("protocol: %s.kind %q is not valid", path, s.Kind))
		}
		if !s.Participation.Valid() {
			errs = append(errs, fmt.Errorf("protocol: %s.participation %q is not valid", path, s.Participation))
		}
		checkDescription(&errs, path, s.Description)
	}
	presets := map[string]bool{}
	for i, pr := range p.Presets {
		path := fmt.Sprintf("presets[%d]", i)
		if pr.PresetID == "" || presets[pr.PresetID] {
			errs = append(errs, fmt.Errorf("protocol: %s.presetId is empty or duplicated", path))
		}
		presets[pr.PresetID] = true
		checkDescription(&errs, path, pr.Description)
	}
	return errors.Join(errs...)
}
