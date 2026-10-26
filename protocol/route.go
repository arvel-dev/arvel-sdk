// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
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

// RouteType is a task type the router may choose. Roles maps a slot to its
// default state in this type; missing slots are "off" (PROTOCOL §14.3).
type RouteType struct {
	TypeID      string               `json:"typeId"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Roles       map[string]SlotState `json:"roles,omitempty"`
}

// RouteRole is one role in a place of the fixed order. A role added by a
// plugin has the slot "<pluginId>:<roleId>".
type RouteRole struct {
	Slot        string   `json:"slot"`
	RoleID      string   `json:"roleId"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Kind        RoleKind `json:"kind"`
}

// RoutePlanParams are the params of _arvel/route/plan (and route/explain).
type RoutePlanParams struct {
	Task  RouteTask   `json:"task"`
	Types []RouteType `json:"types"`
	Roles []RouteRole `json:"roles"`
}

// TypeDecision is the router's choice of a task type.
type TypeDecision struct {
	TypeID     string  `json:"typeId"`
	Confidence float64 `json:"confidence"`
}

// RoleDecision is the router's decision about one slot, relative to the chosen
// type's defaults.
type RoleDecision struct {
	Slot       string  `json:"slot"`
	Include    bool    `json:"include"`
	Confidence float64 `json:"confidence"`
}

// RoutePlanResult is the result of _arvel/route/plan. A nil Type marshals as
// null ("no type fits").
type RoutePlanResult struct {
	Type  *TypeDecision  `json:"type"`
	Roles []RoleDecision `json:"roles"`
}

// MarshalJSON always writes "roles" as an array.
func (r RoutePlanResult) MarshalJSON() ([]byte, error) {
	type plain RoutePlanResult
	p := plain(r)
	if p.Roles == nil {
		p.Roles = []RoleDecision{}
	}
	return json.Marshal(p)
}

// RouteCandidate is a role that may answer a message.
type RouteCandidate struct {
	RoleID      string   `json:"roleId"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Kind        RoleKind `json:"kind"`
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
	Kind   RoleKind `json:"kind"`
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

// MarshalJSON always writes "lines" as an array.
func (r RouteExplainResult) MarshalJSON() ([]byte, error) {
	type plain RouteExplainResult
	p := plain(r)
	if p.Lines == nil {
		p.Lines = []ExplainLine{}
	}
	return json.Marshal(p)
}

func checkDescription(errs *[]error, path, desc string) {
	if utf8.RuneCountInString(desc) > MaxRoleDescriptionLen {
		*errs = append(*errs, fmt.Errorf("protocol: %s.description is longer than %d characters", path, MaxRoleDescriptionLen))
	}
}

// Validate checks the request the host builds: unique ids and slots, valid
// enums and description lengths (PROTOCOL §14.2, §14.3).
func (p RoutePlanParams) Validate() error {
	var errs []error
	types := map[string]bool{}
	for i, t := range p.Types {
		path := fmt.Sprintf("types[%d]", i)
		if t.TypeID == "" || types[t.TypeID] {
			errs = append(errs, fmt.Errorf("protocol: %s.typeId is empty or duplicated", path))
		}
		types[t.TypeID] = true
		checkDescription(&errs, path, t.Description)
		for _, slot := range slices.Sorted(maps.Keys(t.Roles)) {
			st := t.Roles[slot]
			if slot == "" {
				errs = append(errs, fmt.Errorf("protocol: %s.roles has an empty slot", path))
			}
			if !st.Valid() {
				errs = append(errs, fmt.Errorf("protocol: %s.roles[%q] state %q is not valid", path, slot, st))
			}
		}
	}
	slots := map[string]bool{}
	for i, r := range p.Roles {
		path := fmt.Sprintf("roles[%d]", i)
		if r.Slot == "" || slots[r.Slot] {
			errs = append(errs, fmt.Errorf("protocol: %s.slot is empty or duplicated", path))
		}
		slots[r.Slot] = true
		if r.RoleID == "" {
			errs = append(errs, fmt.Errorf("protocol: %s.roleId is empty", path))
		}
		if !r.Kind.Valid() {
			errs = append(errs, fmt.Errorf("protocol: %s.kind %q is not valid", path, r.Kind))
		}
		checkDescription(&errs, path, r.Description)
	}
	return errors.Join(errs...)
}
