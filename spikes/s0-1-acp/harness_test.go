// SPDX-License-Identifier: Apache-2.0

package spike

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// recordedCall is one extension method call or notification seen by a peer.
type recordedCall struct {
	Method string
	Params json.RawMessage
}

type extHandler func(ctx context.Context, params json.RawMessage) (any, error)

// testAgent is the agent side of the in-memory pair. The embedded nil
// interface panics on any method a probe did not define, which is the desired
// loud failure.
type testAgent struct {
	acp.Agent

	mu        sync.Mutex
	conn      *acp.AgentSideConnection
	sessions  int
	ext       map[string]extHandler
	extCalls  []recordedCall
	initMeta  map[string]any
	newMeta   map[string]any
	promptFn  func(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error)
	respMeta  map[string]any
	extNotify chan recordedCall
}

func newTestAgent() *testAgent {
	return &testAgent{
		ext:       map[string]extHandler{},
		extNotify: make(chan recordedCall, 16),
	}
}

func (a *testAgent) setExt(method string, h extHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ext[method] = h
}

func (a *testAgent) Initialize(_ context.Context, p acp.InitializeRequest) (acp.InitializeResponse, error) {
	a.mu.Lock()
	a.initMeta = p.Meta
	a.mu.Unlock()
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersion(acp.ProtocolVersionNumber),
		AuthMethods:     []acp.AuthMethod{},
		Meta:            map[string]any{"arvel": map[string]any{"pluginApi": "1", "k": "v"}},
	}, nil
}

func (a *testAgent) NewSession(_ context.Context, p acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.newMeta = p.Meta
	a.sessions++
	return acp.NewSessionResponse{SessionId: acp.SessionId(fmt.Sprintf("s_%d", a.sessions))}, nil
}

func (a *testAgent) Prompt(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	a.mu.Lock()
	fn := a.promptFn
	meta := a.respMeta
	a.mu.Unlock()
	if fn != nil {
		return fn(ctx, p)
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn, Meta: meta}, nil
}

func (a *testAgent) Cancel(context.Context, acp.CancelNotification) error { return nil }

func (a *testAgent) HandleExtensionMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	a.mu.Lock()
	h := a.ext[method]
	call := recordedCall{Method: method, Params: append(json.RawMessage(nil), params...)}
	a.extCalls = append(a.extCalls, call)
	a.mu.Unlock()
	select {
	case a.extNotify <- call:
	default:
	}
	if h == nil {
		return nil, acp.NewMethodNotFound(method)
	}
	return h(ctx, params)
}

// testClient is the client side of the in-memory pair.
type testClient struct {
	acp.Client

	mu           sync.Mutex
	permissionFn func(ctx context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)
	extNotify    chan recordedCall
	updates      chan acp.SessionNotification
	permReqs     chan acp.RequestPermissionRequest
}

func newTestClient() *testClient {
	return &testClient{
		extNotify: make(chan recordedCall, 16),
		updates:   make(chan acp.SessionNotification, 256),
		permReqs:  make(chan acp.RequestPermissionRequest, 16),
	}
}

func (c *testClient) SessionUpdate(_ context.Context, n acp.SessionNotification) error {
	c.updates <- n
	return nil
}

func (c *testClient) RequestPermission(ctx context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	c.permReqs <- p
	c.mu.Lock()
	fn := c.permissionFn
	c.mu.Unlock()
	if fn != nil {
		return fn(ctx, p)
	}
	return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(p.Options[0].OptionId)}, nil
}

func (c *testClient) HandleExtensionMethod(_ context.Context, method string, params json.RawMessage) (any, error) {
	select {
	case c.extNotify <- recordedCall{Method: method, Params: append(json.RawMessage(nil), params...)}:
	default:
	}
	return nil, nil
}

// safeBuffer collects the bytes a peer reads from the wire.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func (b *safeBuffer) From(offset int) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	if offset >= b.buf.Len() {
		return nil
	}
	return append([]byte(nil), b.buf.Bytes()[offset:]...)
}

// pair is an agent connected to a client through two in-memory pipes.
type pair struct {
	agent      *acp.AgentSideConnection
	client     *acp.ClientSideConnection
	agentImpl  *testAgent
	clientImpl *testClient
	// clientToAgent holds every byte the agent read from the client.
	clientToAgent *safeBuffer
	// close shuts all four pipe ends; it is idempotent and also runs at cleanup.
	close func()
}

func newPair(t *testing.T) *pair {
	t.Helper()
	c2aR, c2aW := io.Pipe()
	a2cR, a2cW := io.Pipe()

	tap := &safeBuffer{}
	ag := newTestAgent()
	cl := newTestClient()
	agentConn := acp.NewAgentSideConnection(ag, a2cW, io.TeeReader(c2aR, tap))
	ag.conn = agentConn
	clientConn := acp.NewClientSideConnection(cl, c2aW, a2cR)

	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			_ = c2aW.Close()
			_ = a2cW.Close()
			_ = c2aR.Close()
			_ = a2cR.Close()
		})
	}
	t.Cleanup(closeAll)
	return &pair{agent: agentConn, client: clientConn, agentImpl: ag, clientImpl: cl, clientToAgent: tap, close: closeAll}
}

// logProbe prints the stable verdict line FINDINGS cites: PROBE <n> <YES|NO>: <detail>.
func logProbe(t *testing.T, probe int, yes bool, detail string) {
	t.Helper()
	answer := "NO"
	if yes {
		answer = "YES"
	}
	t.Logf("PROBE %d %s: %s", probe, answer, detail)
}

// truncate shortens raw wire evidence for logs.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// arvelMetaOK reports whether m carries {"arvel": {"k": "v"}}.
func arvelMetaOK(m map[string]any) bool {
	inner, ok := m["arvel"].(map[string]any)
	return ok && inner["k"] == "v"
}

func arvelMeta() map[string]any {
	return map[string]any{"arvel": map[string]any{"k": "v"}}
}
