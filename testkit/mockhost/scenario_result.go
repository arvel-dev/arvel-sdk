// SPDX-License-Identifier: Apache-2.0

package mockhost

import "github.com/arvel-dev/arvel-sdk/protocol"

// StepResult is the outcome of one scenario step.
type StepResult struct {
	Call string `json:"call"`
	Pass bool   `json:"pass"`
	Err  string `json:"error,omitempty"`
}

// ScenarioResult is the outcome of one scenario run.
type ScenarioResult struct {
	Name  string       `json:"name"`
	Pass  bool         `json:"pass"`
	Steps []StepResult `json:"steps"`
	// Updates holds every session/update notification collected during the run,
	// keyed by session id. It lets the conformance CLI inspect what the plugin
	// streamed back (for example a fake vendor's reported environment).
	Updates map[string][]map[string]any `json:"updates,omitempty"`
}

// Summary returns the number of passing and failing steps.
func (r ScenarioResult) Summary() (passed, failed int) {
	for _, s := range r.Steps {
		if s.Pass {
			passed++
		} else {
			failed++
		}
	}
	return passed, failed
}

// ScenarioOptions customize how a scenario is run against a Driver.
type ScenarioOptions struct {
	// Tmpdir is the value of the {{tmpdir}} placeholder.
	Tmpdir string
	// FakevendorDir is the directory holding fakevendor scripts, resolved as
	// <dir>/<name>.yaml for the {{fakevendor:name}} placeholder.
	FakevendorDir string
	// Sandbox, when non-nil, is injected into initialize._meta.arvel.sandbox
	// (PROTOCOL §15). The proxy check uses it to send a sandbox.proxy.
	Sandbox *protocol.SandboxInfo
}
