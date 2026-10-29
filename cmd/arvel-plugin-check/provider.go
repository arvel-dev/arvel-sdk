// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arvel-dev/arvel-sdk/jsonl"
	"github.com/arvel-dev/arvel-sdk/manifest"
	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/testkit/mockhost"
)

// The SPEC §5.10 deadlines. They are variables (not constants) so tests can
// shorten the slow paths; the tests in this package never run in parallel.
var (
	initTimeout     = 5 * time.Second  // initialize must answer within 5 s
	shutdownTimeout = 10 * time.Second // shutdown must exit 0 within 10 s
)

// runProvider runs the provider conformance suite against a binary.
func runProvider(ctx context.Context, f *flags, m *manifest.Manifest) *Report {
	rep := &Report{Plugin: f.binary, Kind: string(m.Type), Level: f.level}

	// The manifest is the gate: without a valid manifest we cannot know how to
	// drive the plugin, so the remaining checks are reported skipped.
	rep.add(checkManifest(f.manifest))
	if rep.Failed > 0 {
		return rep
	}

	for _, c := range providerBasic(ctx, f, m) {
		rep.add(c)
	}
	if f.level == "release" && rep.Failed == 0 {
		for _, c := range providerRelease(ctx, f, m) {
			rep.add(c)
		}
	}
	return rep
}

// providerBasic runs the SPEC §5.10 basic checks, in order.
func providerBasic(ctx context.Context, f *flags, m *manifest.Manifest) (out []CheckResult) {
	local := *f
	local.loaded = m
	r, err := prepareCheck(ctx, &local, "basic", nil, resolveFakevendorDir(f.scenarios), "")
	if err != nil {
		return aborted("test environment", err, nil)
	}
	defer r.finish(&out)
	d, meta, canary := r.d, r.meta, r.canary

	initCtx, cancel := context.WithTimeout(ctx, initTimeout)
	initRes, initErr := d.InitializeCtx(initCtx, meta)
	cancel()
	if initErr != nil {
		out = append(out, fail("initialize", safeError(initErr)))
		out = append(out, skip("status", "initialize failed"))
		out = append(out, skip("models", "initialize failed"))
		out = append(out, skip("unknown method", "initialize failed"))
	} else {
		if initRes.PluginAPI != protocol.PluginAPI {
			out = append(out, fail("initialize", fmt.Sprintf("pluginApi %q, want %q", initRes.PluginAPI, protocol.PluginAPI)))
		} else {
			out = append(out, pass("initialize", fmt.Sprintf("pluginApi=%s", initRes.PluginAPI)))
		}
		out = append(out, checkStatus(d))
		out = append(out, checkModels(d))
		out = append(out, checkUnknownMethod(d))
	}

	code, err := d.ShutdownExit(shutdownTimeout)
	switch {
	case err != nil:
		out = append(out, fail("shutdown", safeError(err)))
	case code != 0:
		out = append(out, fail("shutdown", fmt.Sprintf("exit code %d, want 0", code)))
	default:
		out = append(out, pass("shutdown", "exit code 0"))
	}

	out = append(out, checkStderrJSON(d))
	out = append(out, checkCanaryLeak(canary, d.Stdout()+d.Stderr()))
	redactDetails(out, canary)
	return out
}

// providerRelease runs the release-only checks on top of basic: every scenario
// in --scenarios, then the concurrency, cancel, busy and proxy checks. Each
// check is independent; a failure in one does not abort the others.
func providerRelease(ctx context.Context, f *flags, m *manifest.Manifest) []CheckResult {
	var out []CheckResult
	out = append(out, runScenarios(ctx, f, f.scenarios)...)
	out = append(out, checkConcurrency(ctx, f))
	out = append(out, checkCancel(ctx, f))
	out = append(out, checkBusy(ctx, f))
	out = append(out, runProxyCheck(ctx, f, f.scenarios))
	return out
}

// aborted returns a failing check for the gate that stopped the suite, plus a
// skip for every check that depended on it.
func aborted(name string, err error, dependents []string) []CheckResult {
	out := []CheckResult{fail(name, safeError(err))}
	for _, dep := range dependents {
		out = append(out, skip(dep, name+" failed"))
	}
	return out
}

func checkStatus(d *mockhost.Driver) CheckResult {
	st, err := d.Status()
	if err != nil {
		return fail("status", safeError(err))
	}
	if err := st.Validate(); err != nil {
		return fail("status", safeError(err))
	}
	return pass("status", string(st.State))
}

func checkModels(d *mockhost.Driver) CheckResult {
	mo, err := d.Models(false)
	if err != nil {
		return fail("models", safeError(err))
	}
	if err := mo.Validate(); err != nil {
		return fail("models", safeError(err))
	}
	return pass("models", fmt.Sprintf("%d model(s), default=%s", len(mo.Models), mo.Default))
}

func checkUnknownMethod(d *mockhost.Driver) CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()
	var raw any
	err := d.Call(ctx, "_arvel/conformance_probe", map[string]any{}, &raw)
	var rpcErr *protocol.RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != protocol.CodeMethodNotFound {
		return fail("unknown method", fmt.Sprintf("unknown _arvel/* method: want -32601, got %v", err))
	}
	return pass("unknown method", "unknown _arvel/* method answered -32601")
}

// checkStderrJSON verifies that every non-empty stderr line is a JSON object.
// Empty stderr is tolerated and reported as skipped.
func checkStderrJSON(d *mockhost.Driver) CheckResult {
	s := d.Stderr()
	if strings.TrimSpace(s) == "" {
		return skip("stderr JSON lines", "stderr is empty")
	}
	sc := jsonl.NewScanner(strings.NewReader(s), 0)
	line := 0
	for sc.Scan() {
		line++
		txt := strings.TrimSpace(sc.Text())
		if txt == "" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(txt), &v); err != nil {
			return fail("stderr JSON lines", fmt.Sprintf("line %d is not JSON", line))
		}
		if _, ok := v.(map[string]any); !ok {
			return fail("stderr JSON lines", fmt.Sprintf("line %d is not a JSON object", line))
		}
	}
	if err := sc.Err(); err != nil {
		return fail("stderr JSON lines", safeError(err))
	}
	return pass("stderr JSON lines", fmt.Sprintf("%d line(s)", line))
}
