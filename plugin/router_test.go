// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/logx"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

func readyRouter(t *testing.T, r Router, opts ...Option) *harness {
	t.Helper()
	h := newRouterHarness(t, r, opts...)
	h.mustInitialize(protocol.InitMeta{})
	return h
}

func routerFeatures(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var res struct {
		Meta struct {
			Arvel protocol.InitResultMeta `json:"arvel"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	return res.Meta.Arvel.Features
}

func TestRunRouter_InitializeEmptyCapabilities(t *testing.T) {
	h := newRouterHarness(t, routerAll{newStubRouter(t)})
	raw := h.mustInitialize(protocol.InitMeta{})
	mustEqualJSON(t, json.RawMessage(`{
		"protocolVersion": 1,
		"agentCapabilities": {},
		"agentInfo": {"name": "stub", "version": "0.0.1"},
		"authMethods": [],
		"_meta": {"arvel": {"pluginApi": "1", "features": ["route.plan","route.message","route.effort","route.explain"]}}
	}`), raw)
}

func TestRunRouter_FeaturesFollowInterfaces(t *testing.T) {
	tests := []struct {
		name string
		mk   func(*stubRouter) Router
		want []string
	}{
		{"plan", func(s *stubRouter) Router { return routerPlan{s} }, []string{"route.plan"}},
		{"plan_message", func(s *stubRouter) Router { return routerPlanMessage{s} }, []string{"route.plan", "route.message"}},
		{"effort", func(s *stubRouter) Router { return routerEffort{s} }, []string{"route.effort"}},
		{"all", func(s *stubRouter) Router { return routerAll{s} },
			[]string{"route.plan", "route.message", "route.effort", "route.explain"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newRouterHarness(t, tc.mk(newStubRouter(t)))
			got := routerFeatures(t, h.mustInitialize(protocol.InitMeta{}))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("features = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunRouter_NoFeatureIsError(t *testing.T) {
	err := RunRouter(context.Background(), routerNone{newStubRouter(t)}, WithStdio(strings.NewReader(""), &syncBuffer{}))
	if !errors.Is(err, ErrNoRouteFeature) {
		t.Fatalf("err = %v, want ErrNoRouteFeature", err)
	}
}

func TestRunRouter_SessionMethodsAreMethodNotFound(t *testing.T) {
	h := readyRouter(t, routerAll{newStubRouter(t)})
	for _, m := range []string{protocol.MethodSessionNew, protocol.MethodSessionLoad, protocol.MethodSessionPrompt,
		protocol.MethodSessionSetConfigOption, protocol.MethodModels, protocol.MethodUsage, protocol.MethodAction, protocol.MethodDiscover} {
		if err := h.call(m, map[string]any{}, nil); rpcCode(err) != protocol.CodeMethodNotFound {
			t.Errorf("%s: %v, want -32601", m, err)
		}
	}
	if err := h.client.Notify(protocol.MethodSessionCancel, map[string]any{"sessionId": "s"}); err != nil {
		t.Fatal(err)
	}
	if err := h.call(protocol.MethodStatus, nil, nil); err != nil { // still alive
		t.Fatal(err)
	}
}

func TestRunRouter_NonImplementedRouteIsMethodNotFound(t *testing.T) {
	h := readyRouter(t, routerPlan{newStubRouter(t)})
	err := h.call(protocol.MethodRouteMessage, golden(t, "14.4-route-message-request.json", "/params"), nil)
	if rpcCode(err) != protocol.CodeMethodNotFound {
		t.Fatalf("err = %v, want -32601", err)
	}
}

func TestRoute_Golden(t *testing.T) {
	h := readyRouter(t, routerAll{newStubRouter(t)})
	tests := []struct{ method, req, reqPtr, res, resPtr string }{
		{protocol.MethodRoutePlan, "14.3-route-plan-request.json", "/params", "14.3-route-plan-result.json", "/result"},
		{protocol.MethodRouteMessage, "14.4-route-message-request.json", "/params", "14.4-route-message-result.json", ""},
		{protocol.MethodRouteEffort, "14.5-route-effort-request.json", "/params", "14.5-route-effort-result.json", ""},
		{protocol.MethodRouteExplain, "14.3-route-plan-request.json", "/params", "14.8-route-explain-result.json", ""},
	}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			var got json.RawMessage
			if err := h.call(tc.method, golden(t, tc.req, tc.reqPtr), &got); err != nil {
				t.Fatal(err)
			}
			mustEqualJSON(t, golden(t, tc.res, tc.resPtr), got)
		})
	}
}

func TestRoute_InvalidResultsBecomeInternal(t *testing.T) {
	plan := golden(t, "14.3-route-plan-request.json", "/params")
	msg := golden(t, "14.4-route-message-request.json", "/params")
	eff := golden(t, "14.5-route-effort-request.json", "/params")
	tests := []struct {
		name   string
		method string
		params json.RawMessage
		setup  func(r *stubRouter)
	}{
		{"unknown_step", protocol.MethodRoutePlan, plan, func(r *stubRouter) {
			r.plan = func(context.Context, protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
				return protocol.RoutePlanResult{Steps: []protocol.StepDecision{{StepID: "zzz", Include: true, Confidence: 0.5}}}, nil
			}
		}},
		{"unknown_preset", protocol.MethodRoutePlan, plan, func(r *stubRouter) {
			r.plan = func(context.Context, protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
				return protocol.RoutePlanResult{Preset: &protocol.PresetDecision{PresetID: "nope", Confidence: 0.5}}, nil
			}
		}},
		{"confidence_above_one", protocol.MethodRoutePlan, plan, func(r *stubRouter) {
			r.plan = func(context.Context, protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
				return protocol.RoutePlanResult{Steps: []protocol.StepDecision{{StepID: "s1", Include: true, Confidence: 1.5}}}, nil
			}
		}},
		{"confidence_nan", protocol.MethodRoutePlan, plan, func(r *stubRouter) {
			r.plan = func(context.Context, protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
				return protocol.RoutePlanResult{Steps: []protocol.StepDecision{{StepID: "s1", Include: true, Confidence: math.NaN()}}}, nil
			}
		}},
		{"role_not_in_candidates", protocol.MethodRouteMessage, msg, func(r *stubRouter) {
			r.message = func(context.Context, protocol.RouteMessageParams) (protocol.RouteMessageResult, error) {
				return protocol.RouteMessageResult{RoleID: "r_ghost", Confidence: 0.9}, nil
			}
		}},
		{"effort_not_in_levels", protocol.MethodRouteEffort, eff, func(r *stubRouter) {
			r.effort = func(context.Context, protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
				return protocol.RouteEffortResult{Effort: protocol.EffortMax, Confidence: 0.9}, nil
			}
		}},
		{"explain_empty_text", protocol.MethodRouteExplain, plan, func(r *stubRouter) {
			r.explain = func(context.Context, protocol.RoutePlanParams) (protocol.RouteExplainResult, error) {
				return protocol.RouteExplainResult{Lines: []protocol.ExplainLine{{Text: "", Value: 0.5}}}, nil
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newStubRouter(t)
			tc.setup(r)
			h := readyRouter(t, routerAll{r})
			var got json.RawMessage
			err := h.call(tc.method, tc.params, &got)
			if arvelCode(err) != protocol.ErrInternal || got != nil {
				t.Fatalf("err = %v, result = %s; the invalid result must be withheld", err, got)
			}
			logs := h.stderr.find(t, "router returned an invalid result")
			if len(logs) != 1 || logs[0]["method"] != tc.method {
				t.Fatalf("violation not logged: %v", logs)
			}
			if strings.Contains(h.stderr.String(), "Webhook signature") {
				t.Fatal("request text was logged")
			}
		})
	}
}

func TestRoute_ValidResultWithNullPreset(t *testing.T) {
	r := newStubRouter(t)
	r.plan = func(context.Context, protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
		return protocol.RoutePlanResult{}, nil
	}
	h := readyRouter(t, routerAll{r})
	var got json.RawMessage
	if err := h.call(protocol.MethodRoutePlan, golden(t, "14.3-route-plan-request.json", "/params"), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"preset":null`) {
		t.Fatalf("result = %s", got)
	}
}

func TestRoute_DeadlineExceeded(t *testing.T) {
	r := newStubRouter(t)
	r.plan = func(ctx context.Context, _ protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
		<-ctx.Done()
		return protocol.RoutePlanResult{}, ctx.Err()
	}
	h := readyRouter(t, routerAll{r}, WithRouteTimeout(50*time.Millisecond))
	start := time.Now()
	err := h.call(protocol.MethodRoutePlan, golden(t, "14.3-route-plan-request.json", "/params"), nil)
	var re *protocol.RPCError
	if !errors.As(err, &re) || time.Since(start) > 2*time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
	if d, _ := re.Arvel(); d.Code != protocol.ErrProviderError || d.VendorCode != "timeout" {
		t.Fatalf("error data = %+v", d)
	}
}

func TestRoute_LateResultIsTimeout(t *testing.T) {
	r := newStubRouter(t)
	r.plan = func(ctx context.Context, _ protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
		time.Sleep(150 * time.Millisecond) // ignores the context
		return goldenAs[protocol.RoutePlanResult](t, "14.3-route-plan-result.json", "/result"), nil
	}
	h := readyRouter(t, routerAll{r}, WithRouteTimeout(50*time.Millisecond))
	err := h.call(protocol.MethodRoutePlan, golden(t, "14.3-route-plan-request.json", "/params"), nil)
	var re *protocol.RPCError
	if !errors.As(err, &re) {
		t.Fatal(err)
	}
	if d, _ := re.Arvel(); d.VendorCode != "timeout" {
		t.Fatalf("error data = %+v", d)
	}
}

func TestRoute_ConcurrentCalls(t *testing.T) {
	const n = 20
	var arrived sync.WaitGroup
	arrived.Add(n)
	r := newStubRouter(t)
	r.plan = func(ctx context.Context, _ protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
		arrived.Done()
		done := make(chan struct{})
		go func() { arrived.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			return protocol.RoutePlanResult{}, errors.New("calls did not run concurrently")
		}
		return goldenAs[protocol.RoutePlanResult](t, "14.3-route-plan-result.json", "/result"), nil
	}
	h := readyRouter(t, routerAll{r})
	req := golden(t, "14.3-route-plan-request.json", "/params")
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.call(protocol.MethodRoutePlan, req, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestRoute_RouterErrorAndPanic(t *testing.T) {
	r := newStubRouter(t)
	var calls int
	r.plan = func(context.Context, protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
		calls++
		switch calls {
		case 1:
			return protocol.RoutePlanResult{}, protocol.NewError(protocol.ErrAuthRequired, "key rejected", nil)
		case 2:
			panic("router boom")
		}
		return goldenAs[protocol.RoutePlanResult](t, "14.3-route-plan-result.json", "/result"), nil
	}
	h := readyRouter(t, routerAll{r})
	req := golden(t, "14.3-route-plan-request.json", "/params")
	if err := h.call(protocol.MethodRoutePlan, req, nil); arvelCode(err) != protocol.ErrAuthRequired {
		t.Fatalf("typed error: %v", err)
	}
	if err := h.call(protocol.MethodRoutePlan, req, nil); arvelCode(err) != protocol.ErrInternal {
		t.Fatalf("panic: %v", err)
	}
	if err := h.call(protocol.MethodRoutePlan, req, nil); err != nil {
		t.Fatalf("the router must stay usable: %v", err)
	}
}

func TestRoute_InvalidParams(t *testing.T) {
	h := readyRouter(t, routerAll{newStubRouter(t)})
	err := h.call(protocol.MethodRoutePlan, map[string]any{"task": 7}, nil)
	if rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("err = %v, want -32602", err)
	}
}

func TestRoute_DebugCountersLogged(t *testing.T) {
	t.Setenv(logx.EnvLevel, "debug")
	r := newStubRouter(t)
	var calls int
	r.effort = func(context.Context, protocol.RouteEffortParams) (protocol.RouteEffortResult, error) {
		calls++
		if calls == 2 {
			return protocol.RouteEffortResult{}, errors.New("service down")
		}
		return goldenAs[protocol.RouteEffortResult](t, "14.5-route-effort-result.json", ""), nil
	}
	h := readyRouter(t, routerAll{r})
	req := golden(t, "14.5-route-effort-request.json", "/params")
	for range 3 {
		_ = h.call(protocol.MethodRouteEffort, req, nil)
	}
	lines := h.stderr.find(t, "route call")
	if len(lines) != 3 {
		t.Fatalf("%d route call lines", len(lines))
	}
	last := lines[2]
	if last["calls"] != float64(3) || last["failures"] != float64(1) || last["method"] != protocol.MethodRouteEffort {
		t.Fatalf("last line = %v", last)
	}
}

func TestRoute_NoCounterLinesAtInfo(t *testing.T) {
	h := readyRouter(t, routerAll{newStubRouter(t)})
	if err := h.call(protocol.MethodRouteEffort, golden(t, "14.5-route-effort-request.json", "/params"), nil); err != nil {
		t.Fatal(err)
	}
	if n := len(h.stderr.find(t, "route call")); n != 0 {
		t.Fatalf("%d debug lines at the default level", n)
	}
}

func TestRouter_StatusDebounceAndHost(t *testing.T) {
	r := newStubRouter(t)
	h := readyRouter(t, routerAll{r})
	var mu sync.Mutex
	n := 0
	h.client.HandleNotification(protocol.MethodStatusChanged, func(context.Context, json.RawMessage) {
		mu.Lock()
		n++
		mu.Unlock()
	})
	host := r.getHost()
	st := protocol.Status{State: protocol.StateDegraded, Message: "slow"}
	host.StatusChanged(st)
	host.StatusChanged(st)
	host.StatusChanged(protocol.Status{State: protocol.StateReady})
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return n == 2 })
	if host.Logger() == nil {
		t.Fatal("no logger")
	}
}

func TestRunRouter_ShutdownPaths(t *testing.T) {
	for _, name := range []string{"notification", "stdin_eof", "context_cancel"} {
		t.Run(name, func(t *testing.T) {
			r := newStubRouter(t)
			h := readyRouter(t, routerAll{r})
			switch name {
			case "notification":
				_ = h.client.Notify(protocol.MethodShutdown, nil)
			case "stdin_eof":
				h.closeStdin()
			case "context_cancel":
				h.cancel()
			}
			h.waitDone(5 * time.Second)
			if h.runErr != nil || r.shutdownN.Load() != 1 {
				t.Fatalf("Run = %v, Router.Shutdown calls = %d", h.runErr, r.shutdownN.Load())
			}
		})
	}
}

func TestRunRouter_SecretsRedacted(t *testing.T) {
	r := newStubRouter(t)
	h := newRouterHarness(t, routerAll{r})
	h.mustInitialize(protocol.InitMeta{Connection: protocol.ConnectionInfo{Secrets: map[string]string{"k": testSecret}}})
	r.getHost().Logger().Error("leak " + testSecret)
	h.shutdown()
	if strings.Contains(h.stderr.String(), testSecret) || !strings.Contains(h.stderr.String(), "‹redacted›") {
		t.Fatalf("stderr:\n%s", h.stderr.String())
	}
}

func TestRunRouter_InitializeErrors(t *testing.T) {
	r := newStubRouter(t)
	r.initialize = func(context.Context, protocol.InitMeta) (RouterFeatures, error) {
		return RouterFeatures{}, protocol.NewError(protocol.ErrAuthRequired, "no key", nil)
	}
	h := newRouterHarness(t, routerAll{r})
	if _, err := h.initialize(protocol.InitMeta{}); arvelCode(err) != protocol.ErrAuthRequired {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.initialize(protocol.InitMeta{PluginAPI: "9"}); rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("err = %v", err)
	}
	if err := h.call(protocol.MethodRoutePlan, golden(t, "14.3-route-plan-request.json", "/params"), nil); rpcCode(err) != protocol.CodeInvalidRequest {
		t.Fatalf("route call before initialize: %v", err)
	}
}
