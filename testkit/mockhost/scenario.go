// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/testkit/fakevendor"
	"github.com/arvel-dev/arvel-sdk/testkit/scenario"
)

// RunScenario loads a scenario file, initializes the host from its connection
// and runs every step, failing the test on the first mismatch.
func (h *Host) RunScenario(t testing.TB, file string) {
	t.Helper()
	sc, err := scenario.LoadFile(file)
	if err != nil {
		t.Fatalf("mockhost: %v", err)
	}

	fv := map[string]string{} // name -> built fakevendor binary path
	resolver := scenario.Resolver{
		Vars:   map[string]string{},
		Tmpdir: t.TempDir(),
		Fakevendor: func(name string) (string, error) {
			if p, ok := fv[name]; ok {
				return p, nil
			}
			script := filepath.Join(filepath.Dir(file), "..", "testdata", "fakevendor", name+".yaml")
			s, err := fakevendor.LoadFile(script)
			if err != nil {
				return "", err
			}
			bin, cleanup, err := fakevendor.Build(context.Background(), s, "fakevendor-"+name)
			if err != nil {
				return "", err
			}
			t.Cleanup(cleanup)
			fv[name] = bin
			return bin, nil
		},
	}

	if _, err := h.Initialize(protocol.InitMeta{Connection: sc.Initialize.Connection}); err != nil {
		t.Fatalf("mockhost: initialize: %v", err)
	}

	var sessionID string
	for i, step := range sc.Steps {
		params, err := resolver.Resolve(step.Params)
		if err != nil {
			t.Fatalf("mockhost: step %d params: %v", i, err)
		}
		if step.AnswerPermissions != "" {
			h.armPermission(step.AnswerPermissions)
		}

		var result any
		callErr := h.call(step.Call, params, &result)
		h.disarmPermission()

		if step.ExpectError != nil {
			var re *protocol.RPCError
			if !errors.As(callErr, &re) {
				t.Fatalf("mockhost: step %d (%s): expected error, got result %v", i, step.Call, result)
			}
			if re.Code != step.ExpectError.Code {
				t.Fatalf("mockhost: step %d (%s): error code %d, want %d", i, step.Call, re.Code, step.ExpectError.Code)
			}
			if step.ExpectError.Arvel != "" {
				d, ok := re.Arvel()
				if !ok || string(d.Code) != step.ExpectError.Arvel {
					t.Fatalf("mockhost: step %d (%s): arvel code %q, want %q", i, step.Call, d.Code, step.ExpectError.Arvel)
				}
			}
			continue
		}
		if callErr != nil {
			t.Fatalf("mockhost: step %d (%s): %v", i, step.Call, callErr)
		}

		if step.Expect != nil {
			got, ok := result.(map[string]any)
			if !ok || !scenario.Subset(step.Expect, got) {
				t.Fatalf("mockhost: step %d (%s): result %v does not contain %v", i, step.Call, result, step.Expect)
			}
		}
		for name, path := range step.Save {
			v, ok := scenario.JSONPath(result, path)
			if !ok {
				t.Fatalf("mockhost: step %d (%s): JSONPath %q not found in %v", i, step.Call, path, result)
			}
			resolver.Vars[name] = fmt.Sprint(v)
		}
		if v, ok := resolver.Vars["session"]; ok {
			sessionID = v
		}
		if len(step.ExpectUpdates) > 0 {
			if err := scenario.MatchUpdates(h.Updates(sessionID), step.ExpectUpdates); err != nil {
				t.Fatalf("mockhost: step %d (%s): %v", i, step.Call, err)
			}
		}
	}
}
