// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/internal/fakeclock"
	"github.com/arvel-dev/arvel-sdk/internal/leakcheck"
	"github.com/arvel-dev/arvel-sdk/jsonrpc"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// syncBuffer is a goroutine-safe buffer of the plugin's stderr or stdout.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Lines parses the buffer as JSON lines.
func (s *syncBuffer) Lines(t testing.TB) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(s.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("not a JSON line: %q", l)
		}
		out = append(out, m)
	}
	return out
}

// find returns the log lines whose msg equals msg.
func (s *syncBuffer) find(t testing.TB, msg string) []map[string]any {
	var out []map[string]any
	for _, m := range s.Lines(t) {
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

type teeWriter struct {
	w   io.Writer
	buf *syncBuffer
}

func (t teeWriter) Write(p []byte) (int, error) {
	t.buf.Write(p)
	return t.w.Write(p)
}

var testStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// harness runs plugin.Run or plugin.RunRouter against a scripted client peer.
type harness struct {
	t      *testing.T
	client *jsonrpc.Peer
	stderr *syncBuffer
	wire   *syncBuffer // raw lines the plugin wrote to stdout
	clock  *fakeclock.Clock
	done   chan struct{}
	runErr error
	cancel context.CancelFunc
	// closeStdin ends the plugin's stdin, as a dying host would.
	closeStdin func()
	// stdin is the plugin's stdin, for raw lines the client peer cannot send.
	stdin io.Writer
	// timeout bounds every call; slow tests raise it.
	timeout time.Duration
}

func newHarness(t *testing.T, p Provider, opts ...Option) *harness {
	t.Helper()
	return startHarness(t, func(ctx context.Context, o []Option) error { return Run(ctx, p, o...) }, opts)
}

func newRouterHarness(t *testing.T, r Router, opts ...Option) *harness {
	t.Helper()
	return startHarness(t, func(ctx context.Context, o []Option) error { return RunRouter(ctx, r, o...) }, opts)
}

func startHarness(t *testing.T, run func(context.Context, []Option) error, opts []Option) *harness {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	h := &harness{
		t: t, stderr: &syncBuffer{}, wire: &syncBuffer{},
		clock: fakeclock.New(testStart), done: make(chan struct{}), timeout: 10 * time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.closeStdin = func() { _ = c2sW.Close() }
	h.stdin = c2sW
	all := append([]Option{
		WithStdio(c2sR, teeWriter{w: s2cW, buf: h.wire}),
		WithStderr(h.stderr), WithClock(h.clock), WithVersion("stub", "0.0.1"),
	}, opts...)
	go func() {
		h.runErr = run(ctx, all)
		close(h.done)
	}()
	h.client = jsonrpc.NewPeer(s2cR, c2sW)
	clientCtx, clientCancel := context.WithCancel(context.Background())
	clientDone := make(chan struct{})
	go func() { _ = h.client.Serve(clientCtx); close(clientDone) }()
	t.Cleanup(func() {
		cancel()
		<-h.done
		_ = c2sW.Close()
		_ = s2cW.Close()
		clientCancel()
		<-clientDone
		leakcheck.Check(t, "plugin.(*providerServer)", "plugin.(*routerServer)", "plugin.(*core)",
			"plugin.(*throttle)", "jsonrpc.(*Peer)", "plugin.Run", "plugin.RunRouter")
	})
	return h
}

func (h *harness) call(method string, params, result any) error {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()
	return h.client.Call(ctx, method, params, result)
}

// initialize sends initialize with meta and returns the raw result.
func (h *harness) initialize(meta protocol.InitMeta) (json.RawMessage, error) {
	h.t.Helper()
	if meta.PluginAPI == "" {
		meta.PluginAPI = protocol.PluginAPI
	}
	var res json.RawMessage
	err := h.call("initialize", map[string]any{
		"protocolVersion": 1,
		"_meta":           protocol.WithMeta(nil, meta),
	}, &res)
	return res, err
}

func (h *harness) mustInitialize(meta protocol.InitMeta) json.RawMessage {
	h.t.Helper()
	res, err := h.initialize(meta)
	if err != nil {
		h.t.Fatalf("initialize: %v", err)
	}
	return res
}

// shutdown sends _arvel/shutdown and waits for Run to return.
func (h *harness) shutdown() {
	h.t.Helper()
	if err := h.client.Notify(protocol.MethodShutdown, nil); err != nil {
		h.t.Fatal(err)
	}
	h.waitDone(5 * time.Second)
}

func (h *harness) waitDone(d time.Duration) {
	h.t.Helper()
	select {
	case <-h.done:
	case <-time.After(d):
		h.t.Fatalf("Run did not return within %v", d)
	}
}

// golden returns the part of a protocol/testdata file at the JSON pointer ptr.
func golden(t testing.TB, file, ptr string) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "protocol", "testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	cur := json.RawMessage(b)
	for _, seg := range strings.Split(strings.Trim(ptr, "/"), "/") {
		if seg == "" {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(cur, &m); err != nil {
			t.Fatalf("%s %s: %v", file, ptr, err)
		}
		cur = m[seg]
	}
	return cur
}

func goldenAs[T any](t testing.TB, file, ptr string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(golden(t, file, ptr), &v); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return v
}
