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
	res, err := h.core.runScenario(sc, ScenarioOptions{
		Tmpdir:        t.TempDir(),
		FakevendorDir: filepath.Join(filepath.Dir(file), "..", "testdata", "fakevendor"),
	})
	if err != nil {
		t.Fatalf("mockhost: %v", err)
	}
	if !res.Pass {
		for _, s := range res.Steps {
			if !s.Pass {
				t.Fatalf("mockhost: scenario %s: step %q: %s", res.Name, s.Call, s.Err)
			}
		}
	}
}

// runScenario runs a loaded scenario against the connection, returning a
// structured result. err is non-nil only for scenario-authoring or
// transport-level failures (bad params, a plugin that died mid-scenario); a
// mismatching expectation is a failing step, not an error.
func (c *core) runScenario(sc *scenario.Scenario, opts ScenarioOptions) (ScenarioResult, error) {
	res := ScenarioResult{Name: sc.Name, Pass: true}
	fv := map[string]string{} // name -> built fakevendor binary path
	var cleanups []func()
	defer func() {
		for _, f := range cleanups {
			f()
		}
	}()
	resolver := scenario.Resolver{
		Vars:   map[string]string{},
		Tmpdir: opts.Tmpdir,
		Fakevendor: func(name string) (string, error) {
			if p, ok := fv[name]; ok {
				return p, nil
			}
			script := filepath.Join(opts.FakevendorDir, name+".yaml")
			s, err := fakevendor.LoadFile(script)
			if err != nil {
				return "", err
			}
			bin, cleanup, err := fakevendor.Build(context.Background(), s, "fakevendor-"+name)
			if err != nil {
				return "", err
			}
			cleanups = append(cleanups, cleanup)
			fv[name] = bin
			return bin, nil
		},
	}

	if _, err := c.Initialize(protocol.InitMeta{Connection: sc.Initialize.Connection}); err != nil {
		return res, fmt.Errorf("mockhost: initialize: %w", err)
	}

	var sessionID string
	for i, step := range sc.Steps {
		sr := StepResult{Call: step.Call, Pass: true}
		params, err := resolver.Resolve(step.Params)
		if err != nil {
			sr.Pass = false
			sr.Err = fmt.Sprintf("params: %v", err)
			res.Steps = append(res.Steps, sr)
			return res, fmt.Errorf("mockhost: step %d params: %w", i, err)
		}
		if step.AnswerPermissions != "" {
			c.armPermission(step.AnswerPermissions)
		}

		var result any
		callErr := c.call(step.Call, params, &result)
		c.disarmPermission()

		if step.ExpectError != nil {
			var re *protocol.RPCError
			switch {
			case !errors.As(callErr, &re):
				sr.Pass = false
				sr.Err = fmt.Sprintf("expected error, got result %v", result)
			case re.Code != step.ExpectError.Code:
				sr.Pass = false
				sr.Err = fmt.Sprintf("error code %d, want %d", re.Code, step.ExpectError.Code)
			case step.ExpectError.Arvel != "":
				d, ok := re.Arvel()
				if !ok || string(d.Code) != step.ExpectError.Arvel {
					sr.Pass = false
					sr.Err = fmt.Sprintf("arvel code %q, want %q", d.Code, step.ExpectError.Arvel)
				}
			}
			res.Steps = append(res.Steps, sr)
			res.Pass = res.Pass && sr.Pass
			continue
		}
		if callErr != nil {
			sr.Pass = false
			sr.Err = callErr.Error()
			res.Steps = append(res.Steps, sr)
			res.Pass = false
			continue
		}

		if step.Expect != nil {
			got, ok := result.(map[string]any)
			if !ok || !scenario.Subset(step.Expect, got) {
				sr.Pass = false
				sr.Err = fmt.Sprintf("result %v does not contain %v", result, step.Expect)
			}
		}
		if sr.Pass {
			for name, path := range step.Save {
				v, ok := scenario.JSONPath(result, path)
				if !ok {
					sr.Pass = false
					sr.Err = fmt.Sprintf("JSONPath %q not found in %v", path, result)
					break
				}
				resolver.Vars[name] = fmt.Sprint(v)
			}
		}
		if sr.Pass {
			if v, ok := resolver.Vars["session"]; ok {
				sessionID = v
			}
			if len(step.ExpectUpdates) > 0 {
				if err := scenario.MatchUpdates(c.Updates(sessionID), step.ExpectUpdates); err != nil {
					sr.Pass = false
					sr.Err = err.Error()
				}
			}
		}
		res.Steps = append(res.Steps, sr)
		res.Pass = res.Pass && sr.Pass
	}
	return res, nil
}
