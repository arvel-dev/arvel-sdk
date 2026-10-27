// SPDX-License-Identifier: Apache-2.0

package fakevendor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Script is a fake vendor CLI program.
type Script struct {
	Name string `yaml:"name"`
	// Version is the raw line emitted to stdout when argv contains --version.
	Version string `yaml:"version,omitempty"`
	// AuthStatus is the raw line emitted to stdout when argv is ["auth","status"].
	AuthStatus string `yaml:"authStatus,omitempty"`
	Steps      []Step `yaml:"steps"`
}

// Step is one action of a script. Exactly one field is set (Validate enforces
// this).
type Step struct {
	Expect      *Expect `yaml:"expect,omitempty"`
	Emit        *Emit   `yaml:"emit,omitempty"`
	SleepMillis int     `yaml:"sleepMillis,omitempty"`
	ExitCode    *int    `yaml:"exitCode,omitempty"`
}

// Expect matches a stdin line, or asserts argv.
type Expect struct {
	// JSON requires the decoded stdin line to contain this subset.
	JSON map[string]any `yaml:"json,omitempty"`
	// Regex requires the stdin line to match this regexp.
	Regex string `yaml:"regex,omitempty"`
	// Argv requires argv to equal this exactly (no line is read).
	Argv []string `yaml:"argv,omitempty"`
}

// Emit writes one line to stdout or stderr.
type Emit struct {
	// Stream is "stdout" (default) or "stderr".
	Stream string `yaml:"stream,omitempty"`
	Line   string `yaml:"line"`
}

// Load decodes a script from r. Unknown fields are an error.
func Load(r io.Reader) (*Script, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var s Script
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("fakevendor: load: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// LoadFile loads a script from a file.
func LoadFile(path string) (*Script, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("fakevendor: load %s: %w", path, err)
	}
	defer f.Close()
	s, err := Load(f)
	if err != nil {
		return nil, fmt.Errorf("fakevendor: load %s: %w", path, err)
	}
	return s, nil
}

// Validate checks the step structure and compiles every regexp.
func (s *Script) Validate() error {
	var errs []error
	if s.Name == "" {
		errs = append(errs, errors.New("fakevendor: name is empty"))
	}
	for i, st := range s.Steps {
		set := 0
		if st.Expect != nil {
			set++
		}
		if st.Emit != nil {
			set++
		}
		if st.SleepMillis > 0 {
			set++
		}
		if st.ExitCode != nil {
			set++
		}
		if set != 1 {
			errs = append(errs, fmt.Errorf("fakevendor: steps[%d] must set exactly one of expect, emit, sleepMillis, exitCode", i))
		}
		if st.Expect != nil {
			if err := validateExpect(st.Expect); err != nil {
				errs = append(errs, fmt.Errorf("fakevendor: steps[%d].expect: %w", i, err))
			}
		}
		if st.Emit != nil && st.Emit.Stream != "" && st.Emit.Stream != "stdout" && st.Emit.Stream != "stderr" {
			errs = append(errs, fmt.Errorf("fakevendor: steps[%d].emit.stream %q is not stdout or stderr", i, st.Emit.Stream))
		}
	}
	return errors.Join(errs...)
}

func validateExpect(e *Expect) error {
	n := 0
	if e.JSON != nil {
		n++
	}
	if e.Regex != "" {
		n++
		if _, err := regexp.Compile(e.Regex); err != nil {
			return fmt.Errorf("invalid regexp %q: %w", e.Regex, err)
		}
	}
	if e.Argv != nil {
		n++
	}
	if n != 1 {
		return errors.New("must set exactly one of json, regex, argv")
	}
	return nil
}
