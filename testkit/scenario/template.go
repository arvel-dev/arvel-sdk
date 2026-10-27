// SPDX-License-Identifier: Apache-2.0

package scenario

import (
	"fmt"
	"strings"
)

// Resolver resolves {{...}} placeholders in scenario values.
type Resolver struct {
	// Vars holds saved variables, referenced as {{name}}.
	Vars map[string]string
	// Tmpdir is the value of {{tmpdir}}.
	Tmpdir string
	// Fakevendor resolves {{fakevendor:name}} to a binary path. It may be nil,
	// in which case a fakevendor placeholder is an error.
	Fakevendor func(name string) (string, error)
}

// ResolveString replaces every {{...}} placeholder in s. An unknown placeholder
// or a missing variable is an error. No escaping is supported.
func (r Resolver) ResolveString(s string) (string, error) {
	var b strings.Builder
	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			b.WriteString(s)
			return b.String(), nil
		}
		j := strings.Index(s[i:], "}}")
		if j < 0 {
			return "", fmt.Errorf("scenario: unterminated placeholder in %q", s)
		}
		b.WriteString(s[:i])
		name := strings.TrimSpace(s[i+2 : i+j])
		v, err := r.lookup(name)
		if err != nil {
			return "", err
		}
		b.WriteString(v)
		s = s[i+j+2:]
	}
}

func (r Resolver) lookup(name string) (string, error) {
	switch {
	case name == "tmpdir":
		return r.Tmpdir, nil
	case strings.HasPrefix(name, "fakevendor:"):
		n := strings.TrimPrefix(name, "fakevendor:")
		if r.Fakevendor == nil {
			return "", fmt.Errorf("scenario: {{fakevendor:%s}} has no fakevendor hook", n)
		}
		return r.Fakevendor(n)
	default:
		if v, ok := r.Vars[name]; ok {
			return v, nil
		}
		return "", fmt.Errorf("scenario: unknown variable {{%s}}", name)
	}
}

// Resolve walks v recursively, replacing placeholders in every string.
func (r Resolver) Resolve(v any) (any, error) {
	switch x := v.(type) {
	case string:
		return r.ResolveString(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			rv, err := r.Resolve(e)
			if err != nil {
				return nil, err
			}
			out[k] = rv
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			rv, err := r.Resolve(e)
			if err != nil {
				return nil, err
			}
			out[i] = rv
		}
		return out, nil
	default:
		return v, nil
	}
}
