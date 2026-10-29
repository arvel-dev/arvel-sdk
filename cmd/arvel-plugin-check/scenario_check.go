// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arvel-dev/arvel-sdk/testkit/mockhost"
	"github.com/arvel-dev/arvel-sdk/testkit/scenario"
)

// runScenarios runs every *.yaml scenario in dir, each against a fresh plugin
// process (a plugin may be initialized only once), and reports per-step rows
// plus one aggregate row per scenario.
func runScenarios(ctx context.Context, f *flags, dir string) []CheckResult {
	files, err := scenarioFiles(dir)
	if err != nil {
		return []CheckResult{fail("scenarios", err.Error())}
	}
	if len(files) == 0 {
		return []CheckResult{skip("scenarios", fmt.Sprintf("no scenarios in %s", dir))}
	}
	fakevendorDir := resolveFakevendorDir(dir)

	var out []CheckResult
	for _, file := range files {
		out = append(out, runOneScenario(ctx, f, file, fakevendorDir)...)
	}
	return out
}

// scenarioFiles returns the *.yaml files of dir in sorted order.
func scenarioFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("scenarios: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || strings.ToLower(filepath.Ext(e.Name())) != ".yaml" {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	return files, nil
}

// resolveFakevendorDir returns <scenariosDir>/../testdata/fakevendor when it
// exists, else an empty dir. Scenarios that do not use {{fakevendor:...}} never
// touch it.
func resolveFakevendorDir(dir string) string {
	d := filepath.Join(dir, "..", "testdata", "fakevendor")
	if st, err := os.Stat(d); err == nil && st.IsDir() {
		return d
	}
	return ""
}

// runOneScenario runs a single scenario file in a fresh driver and returns its
// per-step rows followed by the aggregate scenario row.
func runOneScenario(ctx context.Context, f *flags, file, fakevendorDir string) (out []CheckResult) {
	name := "scenario " + filepath.Base(file)
	sc, err := scenario.LoadFile(file)
	if err != nil {
		return []CheckResult{fail(name, err.Error())}
	}

	r, err := prepareCheck(ctx, f, "scenario", sc, fakevendorDir, "")
	if err != nil {
		return []CheckResult{fail(name, safeError(err))}
	}
	defer r.finish(&out)
	var res mockhost.ScenarioResult
	_, runErr := r.initialize()
	if runErr == nil {
		res, runErr = r.prepared.RunInitialized(ctx, r.d)
	}

	for i, st := range res.Steps {
		stepName := fmt.Sprintf("%s · step %d %s", name, i+1, st.Call)
		if st.Pass {
			out = append(out, pass(stepName, ""))
		} else {
			out = append(out, fail(stepName, st.Err))
		}
	}

	switch {
	case runErr != nil:
		out = append(out, fail(name, safeError(runErr)))
	case res.Pass:
		out = append(out, pass(name, fmt.Sprintf("%d steps", len(res.Steps))))
	default:
		out = append(out, fail(name, firstScenarioFailure(res)))
	}
	return out
}

// firstScenarioFailure names the first failing step of a scenario.
func firstScenarioFailure(res mockhost.ScenarioResult) string {
	for i, st := range res.Steps {
		if !st.Pass {
			return fmt.Sprintf("step %d %q: %s", i+1, st.Call, st.Err)
		}
	}
	return "scenario failed"
}
