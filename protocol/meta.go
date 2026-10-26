// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/json"
	"fmt"
)

const metaKey = "arvel"

// MetaFrom reads "_meta.arvel" into T. It returns false and no error when the
// meta map is nil or has no "arvel" member.
func MetaFrom[T any](meta map[string]any) (T, bool, error) {
	var zero T
	raw, ok := meta[metaKey]
	if !ok {
		return zero, false, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return zero, false, fmt.Errorf("protocol: encode _meta.arvel: %w", err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return zero, false, fmt.Errorf("protocol: decode _meta.arvel: %w", err)
	}
	return out, true, nil
}

// WithMeta returns a copy of meta with "arvel" set to v. The argument is not
// modified and may be nil.
func WithMeta(meta map[string]any, v any) map[string]any {
	out := make(map[string]any, len(meta)+1)
	for k, val := range meta {
		out[k] = val
	}
	out[metaKey] = v
	return out
}
