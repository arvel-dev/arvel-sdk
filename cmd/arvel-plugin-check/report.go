// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// Status is the outcome of one check.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// CheckResult is the outcome of a single conformance check.
type CheckResult struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Report aggregates the results of one conformance run.
type Report struct {
	Plugin  string        `json:"plugin"`
	Kind    string        `json:"kind"`
	Level   string        `json:"level"`
	Passed  int           `json:"passed"`
	Failed  int           `json:"failed"`
	Skipped int           `json:"skipped"`
	Checks  []CheckResult `json:"checks"`
}

// add appends a check and updates the counters.
func (r *Report) add(c CheckResult) {
	switch c.Status {
	case StatusPass:
		r.Passed++
	case StatusFail:
		r.Failed++
	case StatusSkip:
		r.Skipped++
	}
	r.Checks = append(r.Checks, c)
}

// ok reports whether every non-skipped check passed.
func (r *Report) ok() bool { return r.Failed == 0 }

// renderHuman writes one line per check and a summary footer.
func renderHuman(w io.Writer, r *Report) {
	for _, c := range r.Checks {
		switch c.Status {
		case StatusPass:
			fmt.Fprintf(w, "PASS %s\n", c.Name)
		case StatusFail:
			fmt.Fprintf(w, "FAIL %s\n", c.Name)
		case StatusSkip:
			fmt.Fprintf(w, "SKIP %s\n", c.Name)
		}
		if c.Detail != "" {
			fmt.Fprintf(w, "     %s\n", c.Detail)
		}
	}
	fmt.Fprintf(w, "%d passed, %d failed, %d skipped\n", r.Passed, r.Failed, r.Skipped)
}

// renderJSON writes the report as indented JSON followed by a newline.
func renderJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// check helpers build a CheckResult concisely.
func pass(name, detail string) CheckResult { return CheckResult{Name: name, Status: StatusPass, Detail: detail} }
func fail(name, detail string) CheckResult { return CheckResult{Name: name, Status: StatusFail, Detail: detail} }
func skip(name, detail string) CheckResult { return CheckResult{Name: name, Status: StatusSkip, Detail: detail} }
