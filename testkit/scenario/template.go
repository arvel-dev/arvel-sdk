// SPDX-License-Identifier: Apache-2.0

package scenario

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
)

// Resolver resolves {{...}} placeholders in scenario values.
type Resolver struct {
	// Vars holds saved variables, referenced as {{name}}.
	Vars map[string]string
	// Tmpdir is the value of {{tmpdir}}.
	Tmpdir string
	// DataDir and Workspace are static owned-directory placeholders.
	DataDir   string
	Workspace string
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
			if strings.Contains(s, "}}") {
				return "", errors.New("scenario: unmatched placeholder delimiter")
			}
			b.WriteString(s)
			return b.String(), nil
		}
		j := strings.Index(s[i:], "}}")
		if j < 0 {
			return "", errors.New("scenario: unterminated placeholder")
		}
		if strings.Contains(s[:i], "}}") {
			return "", errors.New("scenario: unmatched placeholder delimiter")
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
		if r.Tmpdir == "" {
			return "", errors.New("scenario: tmpdir is unavailable")
		}
		return r.Tmpdir, nil
	case name == "dataDir":
		if r.DataDir == "" {
			return "", errors.New("scenario: dataDir is unavailable")
		}
		return r.DataDir, nil
	case name == "workspace":
		if r.Workspace == "" {
			return "", errors.New("scenario: workspace is unavailable")
		}
		return r.Workspace, nil
	case strings.HasPrefix(name, "fakevendor:"):
		n := strings.TrimPrefix(name, "fakevendor:")
		if n == "" || n == "." || n == ".." || filepath.IsAbs(n) || strings.ContainsAny(n, "/\\:") {
			return "", errors.New("scenario: invalid fake executable name")
		}
		if r.Fakevendor == nil {
			return "", errors.New("scenario: fake executable hook is unavailable")
		}
		return r.Fakevendor(n)
	default:
		if v, ok := r.Vars[name]; ok {
			return v, nil
		}
		return "", errors.New("scenario: unknown or unavailable placeholder")
	}
}

// Resolve walks v recursively, replacing placeholders in every string.
func (r Resolver) Resolve(v any) (any, error) {
	switch x := v.(type) {
	case string:
		return r.ResolveString(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			e := x[k]
			rv, err := r.Resolve(e)
			if err != nil {
				return nil, err
			}
			out[k] = rv
		}
		return out, nil
	case map[string]string:
		out := make(map[string]string, len(x))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			v := x[k]
			rv, err := r.ResolveString(v)
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
