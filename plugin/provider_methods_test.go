// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/internal/jsoneq"
	"github.com/arvel-dev/arvel-sdk/logx"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

func mustEqualJSON(t *testing.T, want, got any) {
	t.Helper()
	w, _ := json.Marshal(want)
	g, _ := json.Marshal(got)
	if ok, diff := jsoneq.Equal(w, g); !ok {
		t.Fatalf("JSON differs:\n%s\nwant %s\ngot  %s", diff, w, g)
	}
}

func readyHarness(t *testing.T, p Provider, opts ...Option) *harness {
	t.Helper()
	h := newHarness(t, p, opts...)
	h.mustInitialize(protocol.InitMeta{})
	return h
}

func TestMethods_StatusModelsUsageActionGolden(t *testing.T) {
	p := newStub(t)
	h := readyHarness(t, p)

	var st json.RawMessage
	if err := h.call(protocol.MethodStatus, nil, &st); err != nil {
		t.Fatal(err)
	}
	mustEqualJSON(t, golden(t, "6.1-status-result.json", ""), st)

	var models json.RawMessage
	if err := h.call(protocol.MethodModels, golden(t, "6.2-models-params.json", ""), &models); err != nil {
		t.Fatal(err)
	}
	mustEqualJSON(t, golden(t, "6.2-models-result.json", ""), models)

	var usage json.RawMessage
	if err := h.call(protocol.MethodUsage, nil, &usage); err != nil {
		t.Fatal(err)
	}
	mustEqualJSON(t, golden(t, "6.3-usage.json", ""), usage)

	p.action = func(_ context.Context, id string) (protocol.ActionResult, error) {
		return goldenAs[protocol.ActionResult](t, "6.4-action-result-"+id+".json", ""), nil
	}
	for _, kind := range []string{"terminal", "url", "done"} {
		var res json.RawMessage
		if err := h.call(protocol.MethodAction, protocol.ActionParams{ID: kind}, &res); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		mustEqualJSON(t, golden(t, "6.4-action-result-"+kind+".json", ""), res)
	}
}

func TestMethods_InvalidResultsBecomeInternal(t *testing.T) {
	p := newStub(t)
	p.status = func(context.Context) (protocol.Status, error) { return protocol.Status{State: "bogus"}, nil }
	p.models = func(context.Context, bool) (protocol.ModelsResult, error) {
		return protocol.ModelsResult{Default: "missing"}, nil
	}
	p.usage = func(context.Context) (protocol.Usage, error) {
		return protocol.Usage{Source: protocol.UsageSourceExact, Status: protocol.UsageStatusOK,
			Windows: []protocol.UsageWindow{{ID: "w", Label: "w", UsedPercent: 120}}}, nil
	}
	p.action = func(context.Context, string) (protocol.ActionResult, error) {
		return protocol.ActionResult{Kind: protocol.ActionKindURL}, nil // no url
	}
	h := readyHarness(t, p)
	for _, tc := range []struct {
		method string
		params any
	}{
		{protocol.MethodStatus, nil}, {protocol.MethodModels, nil}, {protocol.MethodUsage, nil},
		{protocol.MethodAction, protocol.ActionParams{ID: "x"}},
	} {
		err := h.call(tc.method, tc.params, nil)
		if arvelCode(err) != protocol.ErrInternal {
			t.Errorf("%s: err = %v, want internal", tc.method, err)
		}
	}
	if n := len(h.stderr.find(t, "plugin returned an invalid status")); n != 1 {
		t.Fatalf("status violation logged %d times", n)
	}
}

func TestMethods_ModelsRefreshFlag(t *testing.T) {
	p := newStub(t)
	h := readyHarness(t, p)
	if err := h.call(protocol.MethodModels, protocol.ModelsParams{Refresh: true}, nil); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.lastModelsRef {
		t.Fatal("refresh flag lost")
	}
}

func TestMethods_ActionEmptyID(t *testing.T) {
	h := readyHarness(t, newStub(t))
	if err := h.call(protocol.MethodAction, protocol.ActionParams{}, nil); rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("err = %v, want -32602", err)
	}
}

func TestMethods_ProviderErrorsPassThrough(t *testing.T) {
	p := newStub(t)
	reset := time.Date(2026, 1, 1, 18, 40, 0, 0, time.UTC)
	p.usage = func(context.Context) (protocol.Usage, error) {
		return protocol.Usage{}, protocol.NewError(protocol.ErrRateLimited, "5-hour window exhausted",
			map[string]any{"resetsAt": reset, "windowId": "five_hour", "retryable": true})
	}
	h := readyHarness(t, p)
	err := h.call(protocol.MethodUsage, nil, nil)
	var re *protocol.RPCError
	if !asRPC(err, &re) {
		t.Fatal(err)
	}
	d, _ := re.Arvel()
	if d.Code != protocol.ErrRateLimited || d.WindowID != "five_hour" || !d.ResetsAt.Equal(reset) || !d.Retryable {
		t.Fatalf("error data = %+v", d)
	}
}

func TestMethods_UnknownArvelMethod(t *testing.T) {
	h := readyHarness(t, newStub(t))
	if err := h.call("_arvel/nope", nil, nil); rpcCode(err) != protocol.CodeMethodNotFound {
		t.Fatalf("err = %v, want -32601", err)
	}
	if err := h.call(protocol.MethodDiscover, protocol.DiscoverParams{}, nil); rpcCode(err) != protocol.CodeMethodNotFound {
		t.Fatalf("discover without Discoverer: %v, want -32601", err)
	}
}

func discoverer(p *stubProvider, f func(context.Context, protocol.DiscoverParams) (protocol.DiscoverResult, error)) Provider {
	return stubDiscoverer{stubProvider: p, discover: f}
}

func TestMethods_DiscoverGoldenAndFeature(t *testing.T) {
	p := newStub(t)
	want := goldenAs[protocol.DiscoverResult](t, "7.9-discover-result.json", "/result")
	var gotParams protocol.DiscoverParams
	h := newHarness(t, discoverer(p, func(_ context.Context, dp protocol.DiscoverParams) (protocol.DiscoverResult, error) {
		gotParams = dp
		return want, nil
	}))
	raw := h.mustInitialize(protocol.InitMeta{})
	if !strings.Contains(string(raw), `"discover"`) {
		t.Fatalf("discover feature missing: %s", raw)
	}
	var res json.RawMessage
	req := golden(t, "7.9-discover-request.json", "/params")
	if err := h.call(protocol.MethodDiscover, req, &res); err != nil {
		t.Fatal(err)
	}
	mustEqualJSON(t, golden(t, "7.9-discover-result.json", "/result"), res)
	if len(gotParams.Kinds) != 2 || len(gotParams.Projects) != 2 {
		t.Fatalf("params = %+v", gotParams)
	}
}

func TestMethods_DiscoverSanitises(t *testing.T) {
	p := newStub(t)
	h := newHarness(t, discoverer(p, func(context.Context, protocol.DiscoverParams) (protocol.DiscoverResult, error) {
		return protocol.DiscoverResult{McpServers: []protocol.DiscoveredMcp{{
			Name: "pg", Scope: protocol.DiscoverScopeUser, Transport: protocol.McpTransportStdio,
			Command: []string{"server", "--key=" + testSecret},
			Env: map[string]protocol.SecretField{
				"GOOD":  {Secret: true},
				"REF":   {EnvRef: "HOME"},
				"EMPTY": {}, // neither secret nor envRef: dropped
			},
		}}}, nil
	}))
	h.mustInitialize(protocol.InitMeta{Connection: protocol.ConnectionInfo{Secrets: map[string]string{"k": testSecret}}})
	var res protocol.DiscoverResult
	if err := h.call(protocol.MethodDiscover, protocol.DiscoverParams{Kinds: []protocol.DiscoverKind{protocol.DiscoverKindMCP}}, &res); err != nil {
		t.Fatal(err)
	}
	m := res.McpServers[0]
	if strings.Contains(strings.Join(m.Command, " "), testSecret) {
		t.Fatalf("secret in command: %v", m.Command)
	}
	if _, kept := m.Env["EMPTY"]; kept || len(m.Env) != 2 {
		t.Fatalf("env = %+v", m.Env)
	}
	if res.Skills == nil {
		t.Fatal("skills must be an empty array, not null")
	}
}

func TestMethods_DiscoverInvalidResult(t *testing.T) {
	p := newStub(t)
	h := readyHarness(t, discoverer(p, func(context.Context, protocol.DiscoverParams) (protocol.DiscoverResult, error) {
		return protocol.DiscoverResult{Skills: []protocol.DiscoveredSkill{{Name: "", Path: ""}}}, nil
	}))
	if err := h.call(protocol.MethodDiscover, protocol.DiscoverParams{}, nil); arvelCode(err) != protocol.ErrInternal {
		t.Fatalf("err = %v, want internal", err)
	}
}

func TestMethods_StatusMessageIsRedacted(t *testing.T) {
	p := newStub(t)
	p.status = func(context.Context) (protocol.Status, error) {
		return protocol.Status{State: protocol.StateReady, Message: "key " + testSecret, BillingNote: testSecret}, nil
	}
	h := newHarness(t, p)
	h.mustInitialize(protocol.InitMeta{Connection: protocol.ConnectionInfo{Secrets: map[string]string{"k": testSecret}}})
	var st protocol.Status
	if err := h.call(protocol.MethodStatus, nil, &st); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(st.Message, testSecret) || strings.Contains(st.BillingNote, testSecret) {
		t.Fatalf("status not redacted: %+v", st)
	}
	var mu sync.Mutex
	var changed []protocol.Status
	h.client.HandleNotification(protocol.MethodStatusChanged, func(_ context.Context, raw json.RawMessage) {
		var s protocol.Status
		_ = json.Unmarshal(raw, &s)
		mu.Lock()
		changed = append(changed, s)
		mu.Unlock()
	})
	p.getHost().StatusChanged(protocol.Status{State: protocol.StateDegraded, Message: testSecret})
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(changed) == 1 })
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(changed[0].Message, testSecret) {
		t.Fatalf("status_changed not redacted: %+v", changed[0])
	}
}

func TestHost_NotificationsReachClient(t *testing.T) {
	p := newStub(t)
	h := readyHarness(t, p)
	var mu sync.Mutex
	var statuses []protocol.Status
	var usages []protocol.Usage
	h.client.HandleNotification(protocol.MethodStatusChanged, func(_ context.Context, raw json.RawMessage) {
		var s protocol.Status
		_ = json.Unmarshal(raw, &s)
		mu.Lock()
		statuses = append(statuses, s)
		mu.Unlock()
	})
	h.client.HandleNotification(protocol.MethodUsageUpdated, func(_ context.Context, raw json.RawMessage) {
		var u protocol.Usage
		_ = json.Unmarshal(raw, &u)
		mu.Lock()
		usages = append(usages, u)
		mu.Unlock()
	})
	host := p.getHost()
	host.StatusChanged(protocol.Status{State: protocol.StateNeedsLogin, Message: "sign in"})
	host.StatusChanged(protocol.Status{State: protocol.StateNeedsLogin, Message: "sign in"}) // duplicate
	u1 := goldenAs[protocol.Usage](t, "6.3-usage.json", "")
	u2 := u1
	u2.Status = protocol.UsageStatusWarning
	host.UsageUpdated(u1) // sent at once
	host.UsageUpdated(u2) // kept for the end of the interval
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(statuses) == 1 && len(usages) == 1 })

	h.shutdown() // flushes u2
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(usages) == 2 })
	mu.Lock()
	defer mu.Unlock()
	if len(statuses) != 1 || usages[1].Status != protocol.UsageStatusWarning {
		t.Fatalf("statuses %d, usages %+v", len(statuses), usages)
	}
}

func TestHost_InvalidDropped(t *testing.T) {
	p := newStub(t)
	h := readyHarness(t, p)
	var mu sync.Mutex
	n := 0
	h.client.HandleNotification(protocol.MethodStatusChanged, func(context.Context, json.RawMessage) {
		mu.Lock()
		n++
		mu.Unlock()
	})
	host := p.getHost()
	host.StatusChanged(protocol.Status{State: "bogus"})
	host.UsageUpdated(protocol.Usage{Source: "bogus"})
	host.StatusChanged(protocol.Status{State: protocol.StateReady}) // a valid one proves the earlier ones were dropped, not delayed
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return n == 1 })
	if len(h.stderr.find(t, "invalid status change dropped")) != 1 || len(h.stderr.find(t, "invalid usage update dropped")) != 1 {
		t.Fatalf("drops not logged:\n%s", h.stderr.String())
	}
}

func TestHost_BeforeInitializeDropped(t *testing.T) {
	t.Setenv(logx.EnvLevel, "debug")
	p := newStub(t)
	h := newHarness(t, p)
	var host Host
	p.initialize = func(context.Context, protocol.InitMeta) (Features, error) {
		host = p.getHost()
		host.StatusChanged(protocol.Status{State: protocol.StateReady}) // during Initialize
		return Features{}, nil
	}
	h.mustInitialize(protocol.InitMeta{})
	if len(h.stderr.find(t, "status change before initialize dropped")) != 1 {
		t.Fatalf("early status not dropped:\n%s", h.stderr.String())
	}
}

// asRPC reports whether err is a *protocol.RPCError and stores it.
func asRPC(err error, target **protocol.RPCError) bool { return errors.As(err, target) }

// waitFor polls cond for up to 2 s.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2s")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
