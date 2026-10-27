// SPDX-License-Identifier: Apache-2.0

package scenario

import (
	"fmt"
	"strings"
)

// Subset reports whether want is a recursive JSON subset of got: every key in
// want exists in got, nested maps recurse, scalars compare equal, and slices
// compare element-wise. Numbers compare after normalising integer-valued
// scalars, so 1 (YAML int) equals 1.0 (JSON float64).
func Subset(want, got map[string]any) bool {
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			return false
		}
		if !subsetValue(wv, gv) {
			return false
		}
	}
	return true
}

func subsetValue(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		return Subset(w, g)
	case []any:
		g, ok := got.([]any)
		if !ok || len(w) != len(g) {
			return false
		}
		for i := range w {
			if !subsetValue(w[i], g[i]) {
				return false
			}
		}
		return true
	default:
		return scalarEqual(want, got)
	}
}

func scalarEqual(want, got any) bool {
	wn, wok := asNumber(want)
	gn, gok := asNumber(got)
	if wok && gok {
		return wn == gn
	}
	return want == got
}

// asNumber reports the float64 value of a numeric scalar.
func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

// JSONPath evaluates a dotted path against a decoded JSON value. It supports
// "$" (the whole value) and dotted paths like "$.sessionId" or "$.a.b".
func JSONPath(root any, path string) (any, bool) {
	if path == "$" || path == "" {
		return root, true
	}
	path = strings.TrimPrefix(path, "$")
	path = strings.Trim(path, ".")
	cur := root
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// MatchUpdates checks that every expected update is satisfied by at least one
// collected update. updates are decoded ACP SessionUpdate objects (the "update"
// field of a session/update notification), each with a top-level "sessionUpdate"
// discriminator and an optional "content" member. Set semantics: no strict
// ordering, extra updates are fine.
func MatchUpdates(updates []map[string]any, expects []UpdateExpect) error {
	for _, e := range expects {
		found := false
		for _, u := range updates {
			if u["sessionUpdate"] != e.SessionUpdate {
				continue
			}
			if e.Subset != nil && !Subset(e.Subset, u) {
				continue
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("scenario: expected an update with sessionUpdate %q (subset %v), got %d updates", e.SessionUpdate, e.Subset, len(updates))
		}
	}
	return nil
}
