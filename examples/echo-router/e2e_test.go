// SPDX-License-Identifier: MIT-0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/internal/leakcheck"
	"github.com/arvel-dev/arvel-sdk/jsonrpc"
	"github.com/arvel-dev/arvel-sdk/manifest"
	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

func TestManifestIsValid(t *testing.T) {
	m, err := manifest.LoadFile("arvel.json")
	if err != nil {
		t.Fatal(err)
	}
	if ps := m.ValidateWith(manifest.Options{Official: true}); len(ps) != 0 {
		t.Fatal(ps)
	}
	if m.Type != manifest.TypeRouter {
		t.Fatalf("type = %s", m.Type)
	}
}

func TestRouterInProcess(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- plugin.RunRouter(ctx, router{}, plugin.WithStdio(c2sR, s2cW), plugin.WithStderr(io.Discard), plugin.WithVersion("echo-router", "0.0.0"))
	}()
	client := jsonrpc.NewPeer(s2cR, c2sW)
	clientCtx, clientCancel := context.WithCancel(context.Background())
	clientDone := make(chan struct{})
	go func() { _ = client.Serve(clientCtx); close(clientDone) }()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = c2sW.Close()
		_ = s2cW.Close()
		clientCancel()
		<-clientDone
		leakcheck.Check(t, "plugin.(*routerServer)", "plugin.(*core)", "jsonrpc.(*Peer)")
	})
	call := func(method string, params, result any) error {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		return client.Call(cctx, method, params, result)
	}

	var init struct {
		Caps json.RawMessage `json:"agentCapabilities"`
		Meta struct {
			Arvel protocol.InitResultMeta `json:"arvel"`
		} `json:"_meta"`
	}
	params := map[string]any{"protocolVersion": 1, "_meta": map[string]any{"arvel": protocol.InitMeta{PluginAPI: protocol.PluginAPI}}}
	if err := call("initialize", params, &init); err != nil {
		t.Fatal(err)
	}
	want := []string{"route.plan", "route.message", "route.effort", "route.explain"}
	if string(init.Caps) != "{}" || !slices.Equal(init.Meta.Arvel.Features, want) {
		t.Fatalf("initialize: caps=%s features=%v", init.Caps, init.Meta.Arvel.Features)
	}
	var st protocol.Status
	if err := call(protocol.MethodStatus, nil, &st); err != nil || st.State != protocol.StateReady {
		t.Fatalf("status: %+v %v", st, err)
	}

	req := roster()
	req.Task.Title = "Add a feature flag"
	var res protocol.RoutePlanResult
	if err := call(protocol.MethodRoutePlan, req, &res); err != nil {
		t.Fatal(err)
	}
	if res.Type == nil || res.Type.TypeID != "feature" || res.Validate(req) != nil {
		t.Fatalf("plan: %+v", res)
	}
	msg := protocol.RouteMessageParams{Message: "ask the tester", Candidates: []protocol.RouteCandidate{{RoleID: "tester", Name: "Tester"}, {RoleID: "developer", Name: "Developer"}}}
	var mres protocol.RouteMessageResult
	if err := call(protocol.MethodRouteMessage, msg, &mres); err != nil || mres.RoleID != "tester" {
		t.Fatalf("message: %+v %v", mres, err)
	}
	eff := protocol.RouteEffortParams{Text: "short", RoleID: "developer", Kind: protocol.RoleKindCode, Levels: []protocol.Effort{protocol.EffortLow, protocol.EffortHigh}}
	var eres protocol.RouteEffortResult
	if err := call(protocol.MethodRouteEffort, eff, &eres); err != nil || eres.Effort != protocol.EffortLow {
		t.Fatalf("effort: %+v %v", eres, err)
	}
	var xres protocol.RouteExplainResult
	if err := call(protocol.MethodRouteExplain, req, &xres); err != nil || len(xres.Lines) != 1 {
		t.Fatalf("explain: %+v %v", xres, err)
	}
	err := call(protocol.MethodSessionNew, map[string]any{"cwd": "/x", "mcpServers": []any{}}, nil)
	var re *protocol.RPCError
	if !asRPC(err, &re) || re.Code != protocol.CodeMethodNotFound {
		t.Fatalf("session/new: %v", err)
	}
	if err := client.Notify(protocol.MethodShutdown, nil); err != nil {
		t.Fatal(err)
	}
}

func asRPC(err error, target **protocol.RPCError) bool { return errors.As(err, target) }
