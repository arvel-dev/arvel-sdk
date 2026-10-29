// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/arvel-dev/arvel-sdk/protocol"
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

	p, err := PrepareScenario(context.Background(), sc, PrepareOptions{Environment: c.environment, Tmpdir: opts.Tmpdir, FakevendorDir: opts.FakevendorDir, Sandbox: opts.Sandbox})
	if err != nil {
		return ScenarioResult{}, err
	}
	c.cleanups = append(c.cleanups, p.Close)
	if _, err := c.Initialize(p.Initialize()); err != nil {
		return ScenarioResult{}, safeCallError("initialize", err)
	}
	return c.runSteps(context.Background(), sc, p.resolver)
}

func (c *core) runSteps(ctx context.Context, sc *scenario.Scenario, resolver scenario.Resolver) (ScenarioResult, error) {
	res := ScenarioResult{Name: sc.Name, Pass: true}

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
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		callErr := c.Call(callCtx, step.Call, params, &result)
		cancel()
		c.disarmPermission()

		if step.ExpectError != nil {
			var re *protocol.RPCError
			switch {
			case !errors.As(callErr, &re):
				sr.Pass = false
				sr.Err = "expected RPC error was not returned"
			case re.Code != step.ExpectError.Code:
				sr.Pass = false
				sr.Err = fmt.Sprintf("error code %d, want %d", re.Code, step.ExpectError.Code)
			case step.ExpectError.Arvel != "":
				d, ok := re.Arvel()
				if !ok || string(d.Code) != step.ExpectError.Arvel {
					sr.Pass = false
					sr.Err = "arvel error code mismatch"
				}
			}
			res.Steps = append(res.Steps, sr)
			res.Pass = res.Pass && sr.Pass
			continue
		}
		if callErr != nil {
			sr.Pass = false
			sr.Err = safeCallError(step.Call, callErr).Error()
			res.Steps = append(res.Steps, sr)
			res.Pass = false
			continue
		}

		if step.Expect != nil {
			got, ok := result.(map[string]any)
			if !ok || !scenario.Subset(step.Expect, got) {
				sr.Pass = false
				sr.Err = "result does not contain expected fields"
			}
		}
		if sr.Pass {
			for name, path := range step.Save {
				v, ok := scenario.JSONPath(result, path)
				if !ok {
					sr.Pass = false
					sr.Err = "saved result path was not found"
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
	res.Updates = c.snapshotUpdates()
	return res, nil
}

// safeCallError preserves error identity while omitting untrusted RPC payloads.
func safeCallError(method string, err error) error {
	var re *protocol.RPCError
	if errors.As(err, &re) {
		return &callError{message: fmt.Sprintf("mockhost: %s: RPC code %d", method, re.Code), cause: err}
	}
	return &callError{message: "mockhost: " + method + ": transport or deadline failure", cause: err}
}

type callError struct {
	message string
	cause   error
}

func (e *callError) Error() string { return e.message }
func (e *callError) Unwrap() error { return e.cause }
