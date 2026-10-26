// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"errors"
	"fmt"
	"net/url"
)

// Status is the result of _arvel/status (PROTOCOL §6.1).
type Status struct {
	State       StatusState  `json:"state"`
	Message     string       `json:"message,omitempty"`
	Binary      *BinaryInfo  `json:"binary,omitempty"`
	Account     *AccountInfo `json:"account,omitempty"`
	BillingNote string       `json:"billingNote,omitempty"`
	Actions     []Action     `json:"actions,omitempty"`
}

// BinaryInfo describes the vendor binary found on the machine.
type BinaryInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

// AccountInfo describes the signed-in account.
type AccountInfo struct {
	Label string `json:"label,omitempty"`
	Plan  string `json:"plan,omitempty"`
}

// Action is something the user can run from the status card.
type Action struct {
	ID    string     `json:"id"`
	Label string     `json:"label"`
	Kind  ActionKind `json:"kind"`
}

// ActionParams are the params of _arvel/action.
type ActionParams struct {
	ID string `json:"id"`
}

// ActionResult is the result of _arvel/action, tagged by Kind.
type ActionResult struct {
	Kind            ActionKind        `json:"kind"`
	Command         []string          `json:"command,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	Explain         string            `json:"explain,omitempty"`
	URL             string            `json:"url,omitempty"`
	AwaitCompletion bool              `json:"awaitCompletion,omitempty"`
	Message         string            `json:"message,omitempty"`
}

// Validate checks the state, the binary name and every action.
func (s Status) Validate() error {
	var errs []error
	if !s.State.Valid() {
		errs = append(errs, fmt.Errorf("protocol: status.state %q is not valid", s.State))
	}
	if s.Binary != nil && s.Binary.Name == "" {
		errs = append(errs, errors.New("protocol: status.binary.name is empty"))
	}
	for i, a := range s.Actions {
		if a.ID == "" || a.Label == "" {
			errs = append(errs, fmt.Errorf("protocol: status.actions[%d]: id and label are required", i))
		}
		if !a.Kind.Valid() {
			errs = append(errs, fmt.Errorf("protocol: status.actions[%d].kind %q is not valid", i, a.Kind))
		}
	}
	return errors.Join(errs...)
}

// Validate checks the fields required by the result kind.
func (a ActionResult) Validate() error {
	switch a.Kind {
	case ActionKindTerminal:
		if len(a.Command) == 0 || a.Command[0] == "" {
			return errors.New("protocol: terminal action needs a command")
		}
	case ActionKindURL:
		u, err := url.Parse(a.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("protocol: url action needs an http(s) url")
		}
	case ActionKindDone:
	default:
		return fmt.Errorf("protocol: action kind %q is not valid", a.Kind)
	}
	return nil
}
