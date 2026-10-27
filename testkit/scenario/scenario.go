// SPDX-License-Identifier: Apache-2.0

package scenario

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// Scenario is one scripted exchange with a plugin (SPEC §5.9).
type Scenario struct {
	Name       string     `yaml:"name"`
	Plugin     Plugin     `yaml:"plugin"`
	Initialize Initialize `yaml:"initialize"`
	Steps      []Step     `yaml:"steps"`
}

// Plugin holds the environment the scenario gives the plugin process.
type Plugin struct {
	// Env maps variable names to values; values may contain {{...}} placeholders.
	Env map[string]string `yaml:"env"`
}

// Initialize holds the connection sent in _meta.arvel of initialize.
type Initialize struct {
	Connection protocol.ConnectionInfo `yaml:"connection"`
}

// Step is one call in a scenario.
type Step struct {
	// Call is the JSON-RPC method, e.g. "_arvel/status" or "session/new".
	Call string `yaml:"call"`
	// Params are the request params, templated before sending.
	Params map[string]any `yaml:"params"`
	// Save maps a variable name to a JSONPath evaluated on the result.
	Save map[string]string `yaml:"save"`
	// Expect is a recursive JSON subset the result must satisfy.
	Expect map[string]any `yaml:"expect"`
	// ExpectError, when set, matches an RPC error result instead of a value.
	ExpectError *ErrorExpect `yaml:"expectError"`
	// ExpectUpdates matches collected session/update notifications.
	ExpectUpdates []UpdateExpect `yaml:"expectUpdates"`
	// AnswerPermissions auto-answers session/request_permission during the step.
	AnswerPermissions string `yaml:"answerPermissions"`
}

// ErrorExpect matches a JSON-RPC error response.
type ErrorExpect struct {
	// Code is the numeric JSON-RPC code, e.g. -32601.
	Code int `yaml:"code"`
	// Arvel is the optional data.arvel.code, e.g. "busy".
	Arvel string `yaml:"arvel"`
}

// UpdateExpect matches one collected session/update notification.
type UpdateExpect struct {
	// SessionUpdate is the ACP update discriminator, e.g. "agent_message_chunk".
	SessionUpdate string `yaml:"sessionUpdate"`
	// Subset is an optional recursive subset of the update object.
	Subset map[string]any `yaml:"subset"`
}

// validAnswerPermissions lists the allowed answerPermissions values.
var validAnswerPermissions = map[string]bool{
	"allow_once":   true,
	"allow_always": true,
	"reject_once":  true,
}

// Load decodes a scenario from r. Unknown fields are an error.
func Load(r io.Reader) (*Scenario, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var s Scenario
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("scenario: load: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// LoadFile loads a scenario from a file.
func LoadFile(path string) (*Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("scenario: load %s: %w", path, err)
	}
	defer f.Close()
	s, err := Load(f)
	if err != nil {
		return nil, fmt.Errorf("scenario: load %s: %w", path, err)
	}
	return s, nil
}

// Validate checks the fields a run needs to be unambiguous.
func (s *Scenario) Validate() error {
	var errs []error
	if s.Name == "" {
		errs = append(errs, errors.New("scenario: name is empty"))
	}
	if len(s.Steps) == 0 {
		errs = append(errs, errors.New("scenario: steps is empty"))
	}
	for i, st := range s.Steps {
		if st.Call == "" {
			errs = append(errs, fmt.Errorf("scenario: steps[%d].call is empty", i))
		}
		if st.AnswerPermissions != "" && !validAnswerPermissions[st.AnswerPermissions] {
			errs = append(errs, fmt.Errorf("scenario: steps[%d].answerPermissions %q is not allow_once, allow_always or reject_once", i, st.AnswerPermissions))
		}
	}
	return errors.Join(errs...)
}
