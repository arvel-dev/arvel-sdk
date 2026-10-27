// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/arvel-dev/arvel-sdk/manifest"
	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/testkit/mockhost"
)

// runRouter runs the router conformance suite against a binary.
func runRouter(ctx context.Context, f *flags, m *manifest.Manifest) *Report {
	rep := &Report{Plugin: f.binary, Kind: string(m.Type), Level: f.level}

	rep.add(checkManifest(f.manifest))
	if rep.Failed > 0 {
		return rep
	}

	for _, c := range routerBasic(ctx, f, m) {
		rep.add(c)
	}
	if f.level == "release" && rep.Failed == 0 {
		for _, c := range routerRelease(ctx, f) {
			rep.add(c)
		}
	}
	return rep
}

// routerBasic runs the §14.9 basic checks for a router: manifest, initialize
// result shape (empty agentCapabilities, router-only features), session/new →
// -32601, status, shutdown, stderr JSON lines and canary leak.
func routerBasic(ctx context.Context, f *flags, m *manifest.Manifest) []CheckResult {
	canary := newCanary()
	dataDir, err := os.MkdirTemp("", "arvel-check-")
	if err != nil {
		return aborted("data dir", err, nil)
	}
	defer os.RemoveAll(dataDir)

	d, err := startDriver(f)
	if err != nil {
		return aborted("start", err, []string{"initialize", "status", "session/new", "shutdown", "stderr JSON lines", "canary leak"})
	}
	defer d.Close()

	var out []CheckResult
	meta := buildInitMeta(m, dataDir, canary)
	initRes, inited := checkRouterInitialize(ctx, d, meta)
	out = append(out, initRes)
	if inited {
		out = append(out, checkStatus(d))
		out = append(out, checkSessionNewRejected(d))
	} else {
		out = append(out, skip("status", "initialize failed"))
		out = append(out, skip("session/new", "initialize failed"))
	}

	code, err := d.ShutdownExit(shutdownTimeout)
	switch {
	case err != nil:
		out = append(out, fail("shutdown", err.Error()))
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

// routerRelease runs the release-only checks on top of basic: every scenario in
// --scenarios, then the randomized route validity check under a 3 s deadline.
func routerRelease(ctx context.Context, f *flags) []CheckResult {
	var out []CheckResult
	out = append(out, runScenarios(ctx, f, f.scenarios)...)
	out = append(out, checkRouteValidity(ctx, f))
	return out
}

// checkRouterInitialize sends initialize once and validates the §14.1 result
// shape, which lives in the raw response (not the typed InitResultMeta): the
// top-level agentCapabilities must be an empty object and _meta.arvel.features
// must announce only route.* features. The second return reports whether the
// plugin reached the initialized state, so dependents can be skipped on a wire
// error but still run on a merely non-conformant result.
func checkRouterInitialize(ctx context.Context, d *mockhost.Driver, meta protocol.InitMeta) (CheckResult, bool) {
	ctx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()
	if meta.PluginAPI == "" {
		meta.PluginAPI = protocol.PluginAPI
	}
	var raw struct {
		AgentCapabilities json.RawMessage `json:"agentCapabilities"`
		Meta              struct {
			Arvel protocol.InitResultMeta `json:"arvel"`
		} `json:"_meta"`
	}
	params := map[string]any{
		"protocolVersion": 1,
		"_meta":           protocol.WithMeta(nil, meta),
	}
	if err := d.Call(ctx, protocol.MethodInitialize, params, &raw); err != nil {
		return fail("initialize", err.Error()), false
	}

	if raw.Meta.Arvel.PluginAPI != protocol.PluginAPI {
		return fail("initialize", fmt.Sprintf("pluginApi %q, want %q", raw.Meta.Arvel.PluginAPI, protocol.PluginAPI)), true
	}
	if string(bytes.TrimSpace(raw.AgentCapabilities)) != "{}" {
		return fail("initialize", "agentCapabilities must be an empty object for a router"), true
	}
	if len(raw.Meta.Arvel.Features) == 0 {
		return fail("initialize", "no route.* features announced"), true
	}
	if bad := firstNonRouterFeature(raw.Meta.Arvel.Features); bad != "" {
		return fail("initialize", fmt.Sprintf("feature %q is not a route.* feature", bad)), true
	}
	return pass("initialize", "pluginApi=1, agentCapabilities={}, route features ok"), true
}

// firstNonRouterFeature returns the first feature that is not a route.* feature,
// or "" when every feature is a route feature.
func firstNonRouterFeature(features []string) string {
	for _, f := range features {
		if !isRouterFeature(f) {
			return f
		}
	}
	return ""
}

func isRouterFeature(f string) bool {
	switch f {
	case protocol.FeatureRoutePlan, protocol.FeatureRouteMessage, protocol.FeatureRouteEffort, protocol.FeatureRouteExplain:
		return true
	}
	return false
}

// checkSessionNewRejected asserts a router answers session/new with -32601
// (routers have no session surface, PROTOCOL §14.1).
func checkSessionNewRejected(d *mockhost.Driver) CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()
	var res any
	err := d.Call(ctx, protocol.MethodSessionNew, map[string]any{"cwd": "/x", "mcpServers": []any{}}, &res)
	var rpcErr *protocol.RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != protocol.CodeMethodNotFound {
		return fail("session/new", fmt.Sprintf("session/new must answer -32601, got %v", err))
	}
	return pass("session/new", "session/new answered -32601")
}
