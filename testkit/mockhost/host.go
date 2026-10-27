// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/internal/leakcheck"
	"github.com/arvel-dev/arvel-sdk/jsonrpc"
	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/procx"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// syncBuffer is a goroutine-safe byte buffer for a plugin's stderr.
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

// Host drives one plugin: a provider or a router, in-process or as a binary.
type Host struct {
	tb    testing.TB
	peer  *jsonrpc.Peer
	proc  *procx.Proc // nil for in-process
	close sync.Once
	done  chan struct{} // closed when Run returns (in-process) or the proc exits

	mu           sync.Mutex
	runErr       error
	updates      map[string][]map[string]any
	permOptionID string
	permSeen     int

	stderr     *syncBuffer
	timeout    time.Duration
	cancel     context.CancelFunc
	peerCancel context.CancelFunc
	peerDone   chan struct{}
	stdinW     *io.PipeWriter // mockhost -> plugin stdin
	stdoutW    *io.PipeWriter // plugin stdout -> mockhost (closed to end our read)
}

const defaultTimeout = 10 * time.Second

// InProcess starts a provider in-process and returns a Host that drives it.
func InProcess(t testing.TB, p plugin.Provider, opts ...plugin.Option) *Host {
	t.Helper()
	return startInProcess(t, func(ctx context.Context, o []plugin.Option) error {
		return plugin.Run(ctx, p, o...)
	}, opts)
}

// InProcessRouter starts a router in-process and returns a Host that drives it.
func InProcessRouter(t testing.TB, r plugin.Router, opts ...plugin.Option) *Host {
	t.Helper()
	return startInProcess(t, func(ctx context.Context, o []plugin.Option) error {
		return plugin.RunRouter(ctx, r, o...)
	}, opts)
}

func startInProcess(t testing.TB, run func(context.Context, []plugin.Option) error, opts []plugin.Option) *Host {
	t.Helper()
	c2sR, c2sW := io.Pipe() // mockhost -> plugin stdin
	s2cR, s2cW := io.Pipe() // plugin stdout -> mockhost
	h := &Host{
		tb:      t,
		stderr:  &syncBuffer{},
		updates: map[string][]map[string]any{},
		done:    make(chan struct{}),
		timeout: defaultTimeout,
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.stdinW = c2sW
	h.stdoutW = s2cW
	all := append([]plugin.Option{
		plugin.WithStdio(c2sR, s2cW),
		plugin.WithStderr(h.stderr),
		plugin.WithVersion("mockhost", "0.0.0"),
	}, opts...)
	go func() {
		err := run(ctx, all)
		h.mu.Lock()
		h.runErr = err
		h.mu.Unlock()
		close(h.done)
	}()
	h.peer = jsonrpc.NewPeer(s2cR, c2sW)
	h.registerHandlers()
	h.peerDone = make(chan struct{})
	peerCtx, peerCancel := context.WithCancel(context.Background())
	h.peerCancel = peerCancel
	go func() { _ = h.peer.Serve(peerCtx); close(h.peerDone) }()
	t.Cleanup(func() {
		h.Close()
		leakcheck.Check(t, "plugin.(*providerServer)", "plugin.(*routerServer)", "plugin.(*core)",
			"plugin.(*throttle)", "jsonrpc.(*Peer)", "plugin.Run", "plugin.RunRouter")
	})
	return h
}

// startOptions configure Start.
type startOptions struct {
	args []string
	dir  string
	env  []string
}

// StartOption configures Start.
type StartOption func(*startOptions)

// WithArgs sets the arguments passed to the plugin binary.
func WithArgs(args ...string) StartOption {
	return func(o *startOptions) { o.args = args }
}

// WithDir sets the working directory of the plugin process.
func WithDir(dir string) StartOption {
	return func(o *startOptions) { o.dir = dir }
}

// WithEnv sets the environment of the plugin process. A nil env means the
// default minimal environment (see procx.Start).
func WithEnv(env []string) StartOption {
	return func(o *startOptions) { o.env = env }
}

// Start launches a plugin binary and returns a Host that drives it. It works
// for both provider and router binaries, which share the same stdio transport.
func Start(t testing.TB, binaryPath string, opts ...StartOption) *Host {
	t.Helper()
	o := startOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	c2sR, c2sW := io.Pipe() // mockhost -> plugin stdin
	s2cR, s2cW := io.Pipe() // plugin stdout -> mockhost
	h := &Host{
		tb:      t,
		stderr:  &syncBuffer{},
		updates: map[string][]map[string]any{},
		done:    make(chan struct{}),
		timeout: defaultTimeout,
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.stdinW = c2sW
	h.stdoutW = s2cW
	proc, err := procx.Start(ctx, procx.Spec{
		Path:   binaryPath,
		Args:   o.args,
		Dir:    o.dir,
		Env:    o.env,
		Stdin:  c2sR,
		Stdout: s2cW,
		Stderr: h.stderr,
	})
	if err != nil {
		cancel()
		t.Fatalf("mockhost: start %s: %v", binaryPath, err)
	}
	h.proc = proc
	h.peer = jsonrpc.NewPeer(s2cR, c2sW)
	h.registerHandlers()
	h.peerDone = make(chan struct{})
	peerCtx, peerCancel := context.WithCancel(context.Background())
	h.peerCancel = peerCancel
	go func() { _ = h.peer.Serve(peerCtx); close(h.peerDone) }()
	t.Cleanup(func() { h.Close() })
	return h
}

// registerHandlers wires the notifications and requests the host answers.
func (h *Host) registerHandlers() {
	h.peer.HandleNotification(protocol.MethodSessionUpdate, func(_ context.Context, raw json.RawMessage) {
		var n struct {
			SessionID string         `json:"sessionId"`
			Update    map[string]any `json:"update"`
		}
		if err := json.Unmarshal(raw, &n); err != nil || n.SessionID == "" {
			return
		}
		h.mu.Lock()
		h.updates[n.SessionID] = append(h.updates[n.SessionID], n.Update)
		h.mu.Unlock()
	})
	h.peer.Handle(protocol.MethodSessionRequestPermission, func(_ context.Context, _ json.RawMessage) (any, error) {
		h.mu.Lock()
		opt := h.permOptionID
		h.permSeen++
		h.mu.Unlock()
		if opt == "" {
			return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
		}
		return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(acp.PermissionOptionId(opt))}, nil
	})
}

// call performs a JSON-RPC call with a timeout.
func (h *Host) call(method string, params, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()
	return h.peer.Call(ctx, method, params, result)
}

// Initialize sends initialize and returns the decoded _meta.arvel.
func (h *Host) Initialize(meta protocol.InitMeta) (protocol.InitResultMeta, error) {
	if meta.PluginAPI == "" {
		meta.PluginAPI = protocol.PluginAPI
	}
	var res struct {
		Meta struct {
			Arvel protocol.InitResultMeta `json:"arvel"`
		} `json:"_meta"`
	}
	err := h.call(protocol.MethodInitialize, map[string]any{
		"protocolVersion": 1,
		"_meta":           protocol.WithMeta(nil, meta),
	}, &res)
	if err != nil {
		return protocol.InitResultMeta{}, err
	}
	return res.Meta.Arvel, nil
}

// Shutdown sends _arvel/shutdown, closes the plugin's stdin (PROTOCOL §2) and
// waits for the plugin to exit. Closing stdin also unblocks the procx stdin
// forwarding goroutine, without which cmd.Wait would hang.
func (h *Host) Shutdown() {
	h.tb.Helper()
	if err := h.peer.Notify(protocol.MethodShutdown, nil); err != nil {
		h.tb.Fatalf("shutdown: %v", err)
	}
	_ = h.stdinW.Close()
	h.waitDone(5 * time.Second)
}

func (h *Host) waitDone(d time.Duration) {
	h.tb.Helper()
	var done <-chan struct{}
	if h.proc != nil {
		done = h.proc.Done()
	} else {
		done = h.done
	}
	select {
	case <-done:
	case <-time.After(d):
		h.tb.Fatalf("plugin did not exit within %v; stderr=%s", d, h.stderr.String())
	}
}

// Close tears the host down. It is idempotent and safe to call from t.Cleanup.
func (h *Host) Close() {
	h.close.Do(func() {
		h.cancel()
		if h.proc == nil {
			select {
			case <-h.done:
			case <-time.After(5 * time.Second):
			}
		}
		_ = h.stdinW.Close()
		_ = h.stdoutW.Close()
		h.peerCancel()
		select {
		case <-h.peerDone:
		case <-time.After(5 * time.Second):
		}
		if h.proc != nil {
			select {
			case <-h.proc.Done():
			case <-time.After(5 * time.Second):
				h.proc.KillTree(5 * time.Second)
			}
			_, _ = h.proc.Wait()
		}
	})
}

// RunErr returns the error plugin.Run / plugin.RunRouter returned, if any.
func (h *Host) RunErr() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runErr
}

// PermissionsSeen reports how many permission requests arrived on the host.
func (h *Host) PermissionsSeen() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.permSeen
}

// armPermission arms the host to answer the next permission request with the
// given option id.
func (h *Host) armPermission(optionID string) {
	h.mu.Lock()
	h.permOptionID = optionID
	h.mu.Unlock()
}

func (h *Host) disarmPermission() {
	h.mu.Lock()
	h.permOptionID = ""
	h.mu.Unlock()
}
