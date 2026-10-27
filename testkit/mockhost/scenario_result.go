// SPDX-License-Identifier: Apache-2.0

package mockhost

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
}
