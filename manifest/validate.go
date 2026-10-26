// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/arvel-dev/arvel-sdk/schema"
)

// Options tunes Validate.
type Options struct {
	// Official makes a missing platform an error. Third-party plugins get a
	// warning, because only official plugins must ship all six platforms.
	Official bool
}

// Validate checks the manifest against the JSON Schema and the semantic rules
// of the protocol, treating it as a third-party plugin. See ValidateWith.
func (m *Manifest) Validate() []Problem { return m.ValidateWith(Options{}) }

// ValidateWith returns schema problems first (sorted), then semantic problems
// in a fixed order. The result is deterministic for identical input.
//
// The schema is checked against the document given to Load, so edits made to
// the struct afterwards are seen by the semantic rules only. Validate a
// manifest you built by hand (without Load) and the struct is marshalled first.
func (m *Manifest) ValidateWith(opts Options) []Problem {
	ps := m.schemaProblems()
	return append(ps, m.semanticProblems(opts)...)
}

func (m *Manifest) schemaProblems() []Problem {
	doc := m.raw
	if doc == nil {
		var err error
		if doc, err = json.Marshal(m); err != nil {
			return []Problem{internalProblem(err)}
		}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return []Problem{internalProblem(err)}
	}
	sch, err := compileSchema()
	if err != nil {
		return []Problem{internalProblem(err)}
	}
	err = sch.Validate(inst)
	if err == nil {
		return nil
	}
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) {
		return []Problem{internalProblem(err)}
	}
	var ps []Problem
	for _, u := range verr.BasicOutput().Errors {
		if u.Error == nil {
			continue
		}
		ps = append(ps, Problem{Severity: SeverityError, Path: u.InstanceLocation, Code: "schema", Message: u.Error.String()})
	}
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].Path != ps[j].Path {
			return ps[i].Path < ps[j].Path
		}
		return ps[i].Message < ps[j].Message
	})
	return ps
}

func compileSchema() (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.JSON()))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schema.ID, doc); err != nil {
		return nil, err
	}
	return c.Compile(schema.ID)
}

func internalProblem(err error) Problem {
	return Problem{Severity: SeverityError, Code: "schema-internal", Message: err.Error()}
}
