// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/internal/jsoneq"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// rpcCode returns the JSON-RPC code of err, or 0.
func rpcCode(err error) int {
	var re *protocol.RPCError
	if errors.As(err, &re) {
		return re.Code
	}
	return 0
}

func arvelCode(err error) protocol.ErrorCode {
	var re *protocol.RPCError
	if errors.As(err, &re) {
		if d, ok := re.Arvel(); ok {
			return d.Code
		}
	}
	return ""
}

// goldenFeatures returns the features that produce the initialize response of
// protocol/testdata/5.1; skills and instruction files are
// read from the golden file.
func goldenFeatures(t *testing.T) Features {
	meta := goldenAs[protocol.InitResultMeta](t, "5.1-initialize-response.json", "/result/_meta/arvel")
	return Features{
		LoadSession: true, PromptImage: true, PromptEmbeddedContext: true, MCPHTTP: true, Subagents: true,
		Skills: meta.Skills, InstructionFiles: meta.InstructionFiles,
	}
}

func TestRun_InitializeGolden(t *testing.T) {
	p := newStub(t)
	p.feats = goldenFeatures(t)
	h := newHarness(t, stubDiscoverer{stubProvider: p, discover: func(context.Context, protocol.DiscoverParams) (protocol.DiscoverResult, error) {
		return protocol.DiscoverResult{}, nil
	}})
	var got json.RawMessage
	if err := h.call("initialize", golden(t, "5.1-initialize-request.json", "/params"), &got); err != nil {
		t.Fatal(err)
	}
	// The golden response names another plugin.
	var want map[string]any
	if err := json.Unmarshal(golden(t, "5.1-initialize-response.json", "/result"), &want); err != nil {
		t.Fatal(err)
	}
	want["agentInfo"] = map[string]any{"name": "stub", "version": "0.0.1"}
	wantRaw, _ := json.Marshal(want)
	if ok, diff := jsoneq.Equal(wantRaw, got); !ok {
		t.Fatalf("initialize result differs from the golden response:\n%s\n%s", diff, got)
	}
	if p.lastInit.Connection.Kind != "subscription" {
		t.Fatalf("provider saw %+v", p.lastInit)
	}
}

func TestRun_FeaturesList(t *testing.T) {
	tests := []struct {
		name  string
		feats Features
		want  []string
	}{
		{"minimal", Features{}, []string{"status", "models", "usage", "actions"}},
		{"all", Features{Subagents: true, LoadSession: true, Skills: &protocol.SkillsInfo{}, MaxParallelSessions: 3},
			[]string{"status", "models", "usage", "actions", "subagents", "resume", "skills", "maxParallelSessions:3"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newStub(t)
			p.feats = tc.feats
			h := newHarness(t, p)
			var res struct {
				Meta struct {
					Arvel protocol.InitResultMeta `json:"arvel"`
				} `json:"_meta"`
			}
			raw := h.mustInitialize(protocol.InitMeta{})
			if err := json.Unmarshal(raw, &res); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(res.Meta.Arvel.Features, tc.want) {
				t.Fatalf("features = %v, want %v", res.Meta.Arvel.Features, tc.want)
			}
		})
	}
}

func TestRun_InitializeRegistersSecrets(t *testing.T) {
	p := newStub(t)
	h := newHarness(t, p)
	h.mustInitialize(protocol.InitMeta{Connection: protocol.ConnectionInfo{
		ID: "c1", Kind: "api_key", Secrets: map[string]string{"apiKey": testSecret},
	}})
	p.getHost().Logger().Error("leak test " + testSecret)
	h.shutdown()
	out := h.stderr.String()
	if strings.Contains(out, testSecret) || !strings.Contains(out, "‹redacted›") {
		t.Fatalf("stderr:\n%s", out)
	}
}

func TestRun_InitializeWithoutArvelMeta(t *testing.T) {
	p := newStub(t)
	h := newHarness(t, p)
	var res json.RawMessage
	if err := h.call("initialize", map[string]any{"protocolVersion": 1}, &res); err != nil {
		t.Fatal(err)
	}
	if p.lastInit.PluginAPI != protocol.PluginAPI {
		t.Fatalf("provider saw %+v", p.lastInit)
	}
}

func TestRun_InitializeWrongPluginAPI(t *testing.T) {
	h := newHarness(t, newStub(t))
	_, err := h.initialize(protocol.InitMeta{PluginAPI: "2"})
	if rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("err = %v, want -32602", err)
	}
}

func TestRun_InitializeTwice(t *testing.T) {
	h := newHarness(t, newStub(t))
	h.mustInitialize(protocol.InitMeta{})
	if _, err := h.initialize(protocol.InitMeta{}); rpcCode(err) != protocol.CodeInvalidRequest {
		t.Fatalf("err = %v, want -32600", err)
	}
}

func TestRun_InitializeError(t *testing.T) {
	p := newStub(t)
	p.initialize = func(context.Context, protocol.InitMeta) (Features, error) {
		return Features{}, protocol.NewError(protocol.ErrAuthRequired, "sign in", nil)
	}
	h := newHarness(t, p)
	_, err := h.initialize(protocol.InitMeta{})
	if arvelCode(err) != protocol.ErrAuthRequired {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.initialize(protocol.InitMeta{}); arvelCode(err) != protocol.ErrAuthRequired {
		t.Fatalf("a failed initialize must allow a retry, got %v", err)
	}
}

func TestRun_InitializeTimeout(t *testing.T) {
	p := newStub(t)
	p.initialize = func(ctx context.Context, _ protocol.InitMeta) (Features, error) {
		<-ctx.Done()
		return Features{}, ctx.Err()
	}
	h := newHarness(t, p, WithInitTimeout(50*time.Millisecond))
	start := time.Now()
	_, err := h.initialize(protocol.InitMeta{})
	if arvelCode(err) != protocol.ErrProviderError || time.Since(start) > 2*time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
}

func TestRun_MethodsBeforeInitializeRejected(t *testing.T) {
	h := newHarness(t, newStub(t))
	for _, m := range []string{protocol.MethodStatus, protocol.MethodModels, protocol.MethodUsage, protocol.MethodSessionNew} {
		err := h.call(m, map[string]any{}, nil)
		if rpcCode(err) != protocol.CodeInvalidRequest {
			t.Fatalf("%s before initialize: %v, want -32600", m, err)
		}
	}
}

func TestRun_SetHostBeforeInitialize(t *testing.T) {
	p := newStub(t)
	h := newHarness(t, p)
	h.mustInitialize(protocol.InitMeta{})
	if !p.hostAtInit {
		t.Fatal("SetHost must run before Initialize")
	}
}

func TestRun_ShutdownPaths(t *testing.T) {
	tests := []struct {
		name string
		stop func(h *harness)
	}{
		{"notification", func(h *harness) { _ = h.client.Notify(protocol.MethodShutdown, nil) }},
		{"context_cancel", func(h *harness) { h.cancel() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newStub(t)
			p.shutdown = func(context.Context) error { return errors.New("shutdown failed") }
			h := newHarness(t, p)
			h.mustInitialize(protocol.InitMeta{})
			tc.stop(h)
			h.waitDone(5 * time.Second)
			if h.runErr != nil {
				t.Fatalf("Run = %v, want nil even when Provider.Shutdown fails", h.runErr)
			}
			if p.shutdownN.Load() != 1 {
				t.Fatalf("Provider.Shutdown called %d times", p.shutdownN.Load())
			}
			if len(h.stderr.find(t, "provider shutdown failed")) != 1 {
				t.Fatal("shutdown failure not logged")
			}
		})
	}
}

func TestRun_StdinEOF(t *testing.T) {
	p := newStub(t)
	h := newHarness(t, p)
	h.mustInitialize(protocol.InitMeta{})
	// Closing the client's writer ends the plugin's stdin.
	h.closeStdin()
	h.waitDone(5 * time.Second)
	if h.runErr != nil || p.shutdownN.Load() != 1 {
		t.Fatalf("Run = %v, shutdown calls = %d", h.runErr, p.shutdownN.Load())
	}
}

func TestRun_ShutdownWithoutInitializeSkipsProviderShutdown(t *testing.T) {
	p := newStub(t)
	h := newHarness(t, p)
	h.shutdown()
	if p.shutdownN.Load() != 0 {
		t.Fatalf("Provider.Shutdown called %d times for a provider that never initialized", p.shutdownN.Load())
	}
}

func TestRun_NilProvider(t *testing.T) {
	if err := Run(context.Background(), nil); err == nil {
		t.Fatal("want an error")
	}
	if err := RunRouter(context.Background(), nil); err == nil {
		t.Fatal("want an error")
	}
}

func TestRun_InteractiveIsMethodNotFound(t *testing.T) {
	h := readyHarness(t, newStub(t))
	if err := h.call("_arvel/interactive", map[string]any{}, nil); rpcCode(err) != protocol.CodeMethodNotFound {
		t.Fatalf("err = %v, want -32601", err)
	}
}
