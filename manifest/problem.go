// SPDX-License-Identifier: Apache-2.0

package manifest

import "fmt"

// Severity of a Problem.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Problem is one finding of Validate. Path is a JSON pointer into arvel.json
// ("/connections/1/kind"), Code a stable slug, Message text for people.
type Problem struct {
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
}

// String formats the problem for terminals.
func (p Problem) String() string {
	return fmt.Sprintf("%s %s [%s]: %s", p.Severity, p.Path, p.Code, p.Message)
}

// HasErrors reports whether any problem has error severity.
func HasErrors(ps []Problem) bool {
	for _, p := range ps {
		if p.Severity == SeverityError {
			return true
		}
	}
	return false
}
