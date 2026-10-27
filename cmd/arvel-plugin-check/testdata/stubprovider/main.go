// SPDX-License-Identifier: Apache-2.0

// Command stubprovider is a test-only provider that fails exactly one
// conformance check, selected by the STUB_MODE environment variable. It is built
// and driven by the arvel-plugin-check tests; it is not shipped.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

type stub struct {
	mode   string
	canary string
	proxy  string
	next   atomic.Int64
}

func (s *stub) Initialize(_ context.Context, in protocol.InitMeta) (plugin.Features, error) {
	s.canary = in.Connection.Secrets["conformance-canary"]
	if in.Sandbox != nil {
		s.proxy = in.Sandbox.Proxy
	}
	if s.mode == "echo-canary" {
		fmt.Fprintf(os.Stderr, "leaked %s\n", s.canary)
	}
	return plugin.Features{LoadSession: true}, nil
}

func (s *stub) Status(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateReady}, nil
}

func (s *stub) Models(context.Context, bool) (protocol.ModelsResult, error) {
	switch s.mode {
	case "duplicate-model":
		return protocol.ModelsResult{
			Models: []protocol.Model{
				{ID: "a", Label: "A", Efforts: []protocol.Effort{protocol.EffortLow}},
				{ID: "a", Label: "B", Efforts: []protocol.Effort{protocol.EffortLow}},
			},
			Default: "a",
		}, nil
	case "unknown-effort":
		return protocol.ModelsResult{
			Models:  []protocol.Model{{ID: "a", Label: "A", Efforts: []protocol.Effort{"turbo"}}},
			Default: "a",
		}, nil
	default:
		return protocol.ModelsResult{
			Models:  []protocol.Model{{ID: "a", Label: "A", Efforts: []protocol.Effort{protocol.EffortLow}, DefaultEffort: protocol.EffortLow}},
			Default: "a",
		}, nil
	}
}

func (s *stub) Usage(context.Context) (protocol.Usage, error) {
	return protocol.Usage{Source: protocol.UsageSourceUnknown, Status: protocol.UsageStatusOK}, nil
}

func (s *stub) Action(context.Context, string) (protocol.ActionResult, error) {
	return protocol.ActionResult{Kind: protocol.ActionKindDone}, nil
}

func (s *stub) NewSession(context.Context, plugin.SessionParams) (plugin.Session, error) {
	return &stubSession{stub: s, id: fmt.Sprintf("s_%d", s.next.Add(1))}, nil
}

func (s *stub) LoadSession(context.Context, plugin.LoadParams) (plugin.Session, error) {
	return &stubSession{stub: s, id: fmt.Sprintf("s_%d", s.next.Add(1))}, nil
}

func (s *stub) Shutdown(context.Context) error {
	if s.mode == "ignore-shutdown" {
		select {} // never return: the process must be killed after the timeout
	}
	return nil
}

// runVendor spawns the fake vendor CLI named by ARVEL_VENDOR with the sandbox
// proxy in its environment and streams each of its stdout lines back as an
// agent_message_chunk update. The fakevendor proxy-check script emits the five
// proxy environment variables, one per line.
func (s *stub) runVendor(out plugin.Output) (plugin.TurnResult, error) {
	vendor := os.Getenv("ARVEL_VENDOR")
	if vendor == "" {
		return plugin.TurnResult{}, fmt.Errorf("stubprovider: ARVEL_VENDOR is not set")
	}
	cmd := exec.Command(vendor)
	cmd.Env = []string{
		"HTTPS_PROXY=" + s.proxy,
		"HTTP_PROXY=" + s.proxy,
		"ALL_PROXY=" + s.proxy,
		"NO_PROXY=" + noProxy,
	}
	// SSH_AUTH_SOCK is deliberately absent.
	b, err := cmd.Output()
	if err != nil {
		return plugin.TurnResult{}, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		if err := out.Update(acp.UpdateAgentMessageText(line), nil); err != nil {
			return plugin.TurnResult{}, err
		}
	}
	return plugin.TurnResult{StopReason: acp.StopReasonEndTurn}, nil
}

// noProxy is the PROTOCOL §15.3 localhost exclusion set the stub forwards to its
// vendor process.
const noProxy = "localhost,127.0.0.1"

type stubSession struct {
	stub   *stub
	id     string
	cancel atomic.Bool
}

func (s *stubSession) ID() string { return s.id }

func (s *stubSession) Prompt(ctx context.Context, _ []acp.ContentBlock, out plugin.Output) (plugin.TurnResult, error) {
	switch s.stub.mode {
	case "block-prompt":
		for {
			if s.cancel.Load() {
				return plugin.TurnResult{StopReason: acp.StopReasonCancelled}, nil
			}
			select {
			case <-ctx.Done():
				return plugin.TurnResult{StopReason: acp.StopReasonCancelled}, nil
			case <-time.After(10 * time.Millisecond):
			}
		}
	case "proxy":
		return s.stub.runVendor(out)
	default:
		return plugin.TurnResult{StopReason: acp.StopReasonEndTurn}, nil
	}
}

func (s *stubSession) SetOption(context.Context, string, string) ([]acp.SessionConfigOption, error) {
	return nil, nil
}

func (s *stubSession) Cancel() { s.cancel.Store(true) }

func (s *stubSession) Close(context.Context) error { return nil }

func main() {
	s := &stub{mode: os.Getenv("STUB_MODE")}
	if err := plugin.Run(context.Background(), s, plugin.WithVersion("stubprovider", "0.0.0")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
