// SPDX-License-Identifier: Apache-2.0

// Package jsoneq compares JSON documents semantically for golden tests.
//
// Object key order is ignored, numbers compare by value, and an object member
// that is null, {} or [] is treated as absent: the protocol examples show empty
// containers that the typed structs omit.
package jsoneq

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// Equal reports whether a and b hold the same JSON value after normalisation.
// When they differ it returns a description of the first difference.
func Equal(a, b []byte) (bool, string) {
	va, err := decode(a)
	if err != nil {
		return false, "left: " + err.Error()
	}
	vb, err := decode(b)
	if err != nil {
		return false, "right: " + err.Error()
	}
	na, _ := normalize(va)
	nb, _ := normalize(vb)
	if reflect.DeepEqual(na, nb) {
		return true, ""
	}
	return false, diff("$", na, nb)
}

func decode(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return v, nil
}

// normalize returns the value without empty object members and whether the
// value itself counts as empty (null, {} or []).
func normalize(v any) (any, bool) {
	switch x := v.(type) {
	case nil:
		return nil, true
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, c := range x {
			n, empty := normalize(c)
			if !empty {
				out[k] = n
			}
		}
		return out, len(out) == 0
	case []any:
		out := make([]any, len(x))
		for i, c := range x {
			out[i], _ = normalize(c)
		}
		return out, len(out) == 0
	}
	return v, false
}

func diff(path string, a, b any) string {
	ma, aok := a.(map[string]any)
	mb, bok := b.(map[string]any)
	if aok && bok {
		keys := map[string]bool{}
		for k := range ma {
			keys[k] = true
		}
		for k := range mb {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			ca, inA := ma[k]
			cb, inB := mb[k]
			switch {
			case !inA:
				return fmt.Sprintf("%s.%s: only on the right (%v)", path, k, cb)
			case !inB:
				return fmt.Sprintf("%s.%s: only on the left (%v)", path, k, ca)
			case !reflect.DeepEqual(ca, cb):
				return diff(path+"."+k, ca, cb)
			}
		}
	}
	sa, aok := a.([]any)
	sb, bok := b.([]any)
	if aok && bok {
		if len(sa) != len(sb) {
			return fmt.Sprintf("%s: array length %d != %d", path, len(sa), len(sb))
		}
		for i := range sa {
			if !reflect.DeepEqual(sa[i], sb[i]) {
				return diff(fmt.Sprintf("%s[%d]", path, i), sa[i], sb[i])
			}
		}
	}
	return fmt.Sprintf("%s: %v != %v", path, a, b)
}
