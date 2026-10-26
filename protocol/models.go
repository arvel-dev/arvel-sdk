// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// ModelsParams are the params of _arvel/models.
type ModelsParams struct {
	Refresh bool `json:"refresh"`
}

// Model is one model offered by a connection (PROTOCOL §6.2).
type Model struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	Description    string   `json:"description,omitempty"`
	Aliases        []string `json:"aliases,omitempty"`
	Efforts        []Effort `json:"efforts"`
	DefaultEffort  Effort   `json:"defaultEffort,omitempty"`
	ContextWindow  int      `json:"contextWindow,omitempty"`
	Inputs         []string `json:"inputs,omitempty"`
	Billing        Billing  `json:"billing,omitempty"`
	RecommendedFor []string `json:"recommendedFor,omitempty"`
	Hidden         bool     `json:"hidden"`
}

// MarshalJSON always writes "efforts" as an array ([] means a fixed effort).
func (m Model) MarshalJSON() ([]byte, error) {
	type plain Model
	p := plain(m)
	if p.Efforts == nil {
		p.Efforts = []Effort{}
	}
	return json.Marshal(p)
}

// ModelsResult is the result of _arvel/models.
type ModelsResult struct {
	Models  []Model `json:"models"`
	Default string  `json:"default"`
}

// Validate checks the shape rules of PROTOCOL §6.2: unique ids, efforts inside
// the vocabulary, defaults that exist.
func (r ModelsResult) Validate() error {
	var errs []error
	if len(r.Models) == 0 {
		errs = append(errs, errors.New("protocol: models is empty"))
	}
	seen := map[string]bool{}
	for i, m := range r.Models {
		if m.ID == "" {
			errs = append(errs, fmt.Errorf("protocol: models[%d].id is empty", i))
		} else if seen[m.ID] {
			errs = append(errs, fmt.Errorf("protocol: models[%d].id %q is duplicated", i, m.ID))
		}
		seen[m.ID] = true
		for j, e := range m.Efforts {
			if !e.Valid() {
				errs = append(errs, fmt.Errorf("protocol: models[%d].efforts[%d] %q is not valid", i, j, e))
			}
		}
		if m.DefaultEffort != "" && !slices.Contains(m.Efforts, m.DefaultEffort) {
			errs = append(errs, fmt.Errorf("protocol: models[%d].defaultEffort %q is not in efforts", i, m.DefaultEffort))
		}
		if m.Billing != "" && !m.Billing.Valid() {
			errs = append(errs, fmt.Errorf("protocol: models[%d].billing %q is not valid", i, m.Billing))
		}
	}
	if r.Default == "" || !seen[r.Default] {
		errs = append(errs, fmt.Errorf("protocol: default %q is not a model id", r.Default))
	}
	return errors.Join(errs...)
}
