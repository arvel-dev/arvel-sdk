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
	Workspace  string     `yaml:"workspace"`
	Steps      []Step     `yaml:"steps"`
}

// Plugin holds the environment the scenario gives the plugin process.
type Plugin struct {
	// Env maps variable names to values; values may contain {{...}} placeholders.
	Env map[string]string `yaml:"env"`
}

// Initialize holds the connection sent in _meta.arvel of initialize.
type Initialize struct {
	Connection  protocol.ConnectionInfo `yaml:"connection"`
	DataDir     string                  `yaml:"dataDir"`
	kindPresent bool
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
		return nil, errors.New("scenario: invalid YAML or field type")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("scenario: expected one YAML document")
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
			errs = append(errs, fmt.Errorf("scenario: steps[%d].answerPermissions must be allow_once, allow_always or reject_once", i))
		}
	}
	return errors.Join(errs...)
}

// KindSpecified distinguishes an omitted kind from an explicitly empty YAML kind.
func (in Initialize) KindSpecified() bool { return in.kindPresent || in.Connection.Kind != "" }

// UnmarshalYAML preserves field presence without changing production wire types.
func (in *Initialize) UnmarshalYAML(n *yaml.Node) error {
	type plain Initialize
	if err := validateInitializeNode(n, false, 0); err != nil {
		return err
	}
	var value plain
	if err := n.Decode(&value); err != nil {
		return errors.New("scenario: invalid initialize field type")
	}
	// Decode the effective shallow YAML mapping to preserve native merge priority:
	// a direct connection replaces a merged connection, including kind presence.
	var fields struct {
		Connection map[string]any `yaml:"connection"`
	}
	if err := n.Decode(&fields); err != nil {
		return errors.New("scenario: invalid connection mapping")
	}
	_, value.kindPresent = fields.Connection["kind"]
	*in = Initialize(value)
	return nil
}

func validateInitializeNode(n *yaml.Node, connection bool, depth int) error {
	if n == nil || depth > 64 {
		return errors.New("scenario: invalid initialize alias")
	}
	if n.Tag == "!!null" {
		return nil
	}
	if n.Kind == yaml.AliasNode {
		return validateInitializeNode(n.Alias, connection, depth+1)
	}
	if n.Kind != yaml.MappingNode {
		return errors.New("scenario: initialize and connection must be mappings")
	}
	for i := 0; i < len(n.Content); i += 2 {
		key, value := n.Content[i].Value, n.Content[i+1]
		if key == "<<" {
			nodes := []*yaml.Node{value}
			if value.Kind == yaml.SequenceNode {
				nodes = value.Content
			}
			for _, merged := range nodes {
				if err := validateInitializeNode(merged, connection, depth+1); err != nil {
					return err
				}
			}
			continue
		}
		if connection {
			switch key {
			case "id", "kind", "config", "secrets":
			default:
				return errors.New("scenario: unknown connection field")
			}
			continue
		}
		switch key {
		case "dataDir":
		case "connection":
			if err := validateInitializeNode(value, true, depth+1); err != nil {
				return err
			}
		default:
			return errors.New("scenario: unknown initialize field")
		}
	}
	return nil
}
