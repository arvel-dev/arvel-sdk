// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/jsonrpc"
	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/procx"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// core drives one plugin connection (provider or router) over stdio. It is
// testing.TB-free: every failure is returned as an error, so it can be used by
// both the testing-oriented Host and the non-testing Driver.
type core struct {
	peer *jsonrpc.Peer
	proc *procx.Proc // nil for in-process

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
	done       chan struct{}  // closed when Run returns (in-process) or the proc exits

	closeOnce sync.Once
}

// startCoreInProcess builds a core that runs the provider/plugin in-process.
func startCoreInProcess(run func(context.Context, []plugin.Option) error, opts []plugin.Option) *core {
	c2sR, c2sW := io.Pipe() // mockhost -> plugin stdin
	s2cR, s2cW := io.Pipe() // plugin stdout -> mockhost
	c := &core{
		stderr:  &syncBuffer{},
		updates: map[string][]map[string]any{},
		done:    make(chan struct{}),
		timeout: defaultTimeout,
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.stdinW = c2sW
	c.stdoutW = s2cW
	all := append([]plugin.Option{
		plugin.WithStdio(c2sR, s2cW),
		plugin.WithStderr(c.stderr),
		plugin.WithVersion("mockhost", "0.0.0"),
	}, opts...)
	go func() {
		err := run(ctx, all)
		c.mu.Lock()
		c.runErr = err
		c.mu.Unlock()
		close(c.done)
	}()
	c.peer = jsonrpc.NewPeer(s2cR, c2sW)
	c.registerHandlers()
	c.peerDone = make(chan struct{})
	peerCtx, peerCancel := context.WithCancel(context.Background())
	c.peerCancel = peerCancel
	go func() { _ = c.peer.Serve(peerCtx); close(c.peerDone) }()
	return c
}

// startCore launches a plugin binary and returns a core that drives it.
func startCore(binaryPath string, opts ...StartOption) (*core, error) {
	o := startOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	c2sR, c2sW := io.Pipe() // mockhost -> plugin stdin
	s2cR, s2cW := io.Pipe() // plugin stdout -> mockhost
	c := &core{
		stderr:  &syncBuffer{},
		updates: map[string][]map[string]any{},
		done:    make(chan struct{}),
		timeout: defaultTimeout,
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.stdinW = c2sW
	c.stdoutW = s2cW
	proc, err := procx.Start(ctx, procx.Spec{
		Path:   binaryPath,
		Args:   o.args,
		Dir:    o.dir,
		Env:    o.env,
		Stdin:  c2sR,
		Stdout: s2cW,
		Stderr: c.stderr,
	})
	if err != nil {
		cancel()
		_ = c2sR.Close()
		_ = c2sW.Close()
		_ = s2cR.Close()
		_ = s2cW.Close()
		return nil, err
	}
	c.proc = proc
	c.peer = jsonrpc.NewPeer(s2cR, c2sW)
	c.registerHandlers()
	c.peerDone = make(chan struct{})
	peerCtx, peerCancel := context.WithCancel(context.Background())
	c.peerCancel = peerCancel
	go func() { _ = c.peer.Serve(peerCtx); close(c.peerDone) }()
	return c, nil
}

// registerHandlers wires the notifications and requests the host answers.
func (c *core) registerHandlers() {
	c.peer.HandleNotification(protocol.MethodSessionUpdate, func(_ context.Context, raw json.RawMessage) {
		var n struct {
			SessionID string         `json:"sessionId"`
			Update    map[string]any `json:"update"`
		}
		if err := json.Unmarshal(raw, &n); err != nil || n.SessionID == "" {
			return
		}
		c.mu.Lock()
		c.updates[n.SessionID] = append(c.updates[n.SessionID], n.Update)
		c.mu.Unlock()
	})
	c.peer.Handle(protocol.MethodSessionRequestPermission, func(_ context.Context, _ json.RawMessage) (any, error) {
		c.mu.Lock()
		opt := c.permOptionID
		c.permSeen++
		c.mu.Unlock()
		if opt == "" {
			return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
		}
		return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(acp.PermissionOptionId(opt))}, nil
	})
}

// Call performs a JSON-RPC call with the given context. The context is the
// caller's deadline; it is not extended with the host's default timeout.
func (c *core) Call(ctx context.Context, method string, params, result any) error {
	return c.peer.Call(ctx, method, params, result)
}

// call performs a JSON-RPC call with the host's default timeout.
func (c *core) call(method string, params, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.Call(ctx, method, params, result)
}

// Initialize sends initialize and returns the decoded _meta.arvel, using the
// host's default timeout.
func (c *core) Initialize(meta protocol.InitMeta) (protocol.InitResultMeta, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.InitializeCtx(ctx, meta)
}

// InitializeCtx sends initialize under the caller's context (deadline).
func (c *core) InitializeCtx(ctx context.Context, meta protocol.InitMeta) (protocol.InitResultMeta, error) {
	if meta.PluginAPI == "" {
		meta.PluginAPI = protocol.PluginAPI
	}
	var res struct {
		Meta struct {
			Arvel protocol.InitResultMeta `json:"arvel"`
		} `json:"_meta"`
	}
	err := c.Call(ctx, protocol.MethodInitialize, map[string]any{
		"protocolVersion": 1,
		"_meta":           protocol.WithMeta(nil, meta),
	}, &res)
	if err != nil {
		return protocol.InitResultMeta{}, err
	}
	return res.Meta.Arvel, nil
}

// shutdown sends _arvel/shutdown, closes the plugin's stdin (PROTOCOL §2) and
// waits for the plugin to exit. Closing stdin also unblocks the procx stdin
// forwarding goroutine, without which cmd.Wait would hang.
func (c *core) shutdown() error {
	_, err := c.shutdownExit(5 * time.Second)
	return err
}

// shutdownExit sends _arvel/shutdown, closes the plugin's stdin and waits up to
// d for the plugin to exit, returning its exit code. A non-zero exit is not an
// error: the caller decides which exit codes are acceptable. An in-process
// plugin has no process, so its code is 0.
func (c *core) shutdownExit(d time.Duration) (int, error) {
	if err := c.peer.Notify(protocol.MethodShutdown, nil); err != nil {
		return -1, err
	}
	_ = c.stdinW.Close()
	if err := c.waitDone(d); err != nil {
		return -1, err
	}
	if c.proc == nil {
		return 0, nil
	}
	return c.proc.Wait()
}

// waitDone waits for the plugin process (or in-process Run) to exit.
func (c *core) waitDone(d time.Duration) error {
	var done <-chan struct{}
	if c.proc != nil {
		done = c.proc.Done()
	} else {
		done = c.done
	}
	select {
	case <-done:
		return nil
	case <-time.After(d):
		return &timeoutError{d: d, stderr: c.stderr.String()}
	}
}

// timeoutError reports that the plugin did not exit in time.
type timeoutError struct {
	d      time.Duration
	stderr string
}

func (e *timeoutError) Error() string {
	return "plugin did not exit within " + e.d.String() + "; stderr=" + e.stderr
}

// Close tears the connection down. It is idempotent and safe to call from
// t.Cleanup or defer.
func (c *core) Close() {
	c.closeOnce.Do(func() {
		c.cancel()
		if c.proc == nil {
			select {
			case <-c.done:
			case <-time.After(5 * time.Second):
			}
		}
		_ = c.stdinW.Close()
		_ = c.stdoutW.Close()
		c.peerCancel()
		select {
		case <-c.peerDone:
		case <-time.After(5 * time.Second):
		}
		if c.proc != nil {
			select {
			case <-c.proc.Done():
			case <-time.After(5 * time.Second):
				c.proc.KillTree(5 * time.Second)
			}
			_, _ = c.proc.Wait()
		}
	})
}

// Stderr returns the captured stderr text of the plugin process.
func (c *core) Stderr() string {
	return c.stderr.String()
}

// RunErr returns the error plugin.Run / plugin.RunRouter returned, if any.
func (c *core) RunErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runErr
}

// PermissionsSeen reports how many permission requests arrived on the host.
func (c *core) PermissionsSeen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.permSeen
}

// armPermission arms the host to answer the next permission request with the
// given option id.
func (c *core) armPermission(optionID string) {
	c.mu.Lock()
	c.permOptionID = optionID
	c.mu.Unlock()
}

func (c *core) disarmPermission() {
	c.mu.Lock()
	c.permOptionID = ""
	c.mu.Unlock()
}
