// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/arvel-dev/arvel-sdk/jsonrpc"
	"github.com/arvel-dev/arvel-sdk/logx"
	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/redact"
)

// core is what the provider server and the router server share: logging with
// redaction, the JSON-RPC peer, initialize bookkeeping and the shutdown trigger.
type core struct {
	o        options
	log      *slog.Logger
	redactor *redact.Redactor
	peer     *jsonrpc.Peer

	initMu   sync.Mutex // serialises initialize
	inited   atomic.Bool
	closing  atomic.Bool
	initMeta protocol.InitMeta

	shutdownReq chan struct{}
	reqOnce     sync.Once
	shutOnce    sync.Once

	debounce statusDebounce
}

func newCore(o options) *core {
	rd := redact.New()
	log := logx.NewWith(o.name, logx.Options{Writer: o.stderr, Redactor: rd})
	in, out := o.in, o.out
	if !o.stdio {
		in, out = os.Stdin, os.Stdout
	}
	popts := []jsonrpc.Option{jsonrpc.WithLogger(log), jsonrpc.WithDrainTimeout(o.closeTimeout)}
	if o.maxLine > 0 {
		popts = append(popts, jsonrpc.WithMaxLine(o.maxLine))
	}
	return &core{
		o:           o,
		log:         log,
		redactor:    rd,
		peer:        jsonrpc.NewPeer(in, out, popts...),
		shutdownReq: make(chan struct{}),
	}
}

// requireInit rejects methods that arrive before a successful initialize.
func (c *core) requireInit() error {
	if !c.inited.Load() {
		return &protocol.RPCError{Code: protocol.CodeInvalidRequest, Message: "not initialized"}
	}
	return nil
}

func invalidParams(format string, args ...any) *protocol.RPCError {
	return &protocol.RPCError{Code: protocol.CodeInvalidParams, Message: "invalid params: " + fmt.Sprintf(format, args...)}
}

// decodeParams unmarshals params into v. Absent or null params leave v alone.
func decodeParams(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return invalidParams("%v", err)
	}
	return nil
}

// decodeInit reads the initialize request: it checks pluginApi, registers the
// connection secrets with the redactor and returns the Arvel metadata.
func (c *core) decodeInit(raw json.RawMessage) (protocol.InitMeta, error) {
	var req struct {
		ProtocolVersion int            `json:"protocolVersion"`
		Meta            map[string]any `json:"_meta"`
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return protocol.InitMeta{}, invalidParams("initialize needs params")
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return protocol.InitMeta{}, invalidParams("%v", err)
	}
	in, ok, err := protocol.MetaFrom[protocol.InitMeta](req.Meta)
	if err != nil {
		return protocol.InitMeta{}, invalidParams("%v", err)
	}
	switch {
	case !ok: // a generic ACP client
		c.log.Debug("initialize without _meta.arvel")
		in = protocol.InitMeta{PluginAPI: protocol.PluginAPI}
	case in.PluginAPI != protocol.PluginAPI:
		c.log.Warn("unsupported pluginApi", "pluginApi", in.PluginAPI)
		return protocol.InitMeta{}, invalidParams("unsupported pluginApi %q; this plugin speaks %q", in.PluginAPI, protocol.PluginAPI)
	}
	for _, secret := range in.Connection.Secrets {
		c.redactor.Add(secret)
	}
	return in, nil
}

// initializeResult is the initialize response. It is an own type, not
// acp.InitializeResponse, so a router can send agentCapabilities: {} exactly.
type initializeResult struct {
	ProtocolVersion   int            `json:"protocolVersion"`
	AgentCapabilities any            `json:"agentCapabilities"`
	AgentInfo         *agentInfo     `json:"agentInfo,omitempty"`
	AuthMethods       []any          `json:"authMethods"`
	Meta              map[string]any `json:"_meta"`
}

type agentInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (c *core) initResult(caps any, meta protocol.InitResultMeta) initializeResult {
	return initializeResult{
		ProtocolVersion:   1,
		AgentCapabilities: caps,
		AgentInfo:         &agentInfo{Name: c.o.name, Version: c.o.version},
		AuthMethods:       []any{},
		Meta:              protocol.WithMeta(nil, meta),
	}
}

// registerCommon registers what both server kinds answer.
func (c *core) registerCommon() {
	c.peer.HandleNotification(protocol.MethodShutdown, func(context.Context, json.RawMessage) {
		c.reqOnce.Do(func() { close(c.shutdownReq) })
	})
}

// run serves the peer until stdin ends, a signal arrives, the context is done or
// the host sends _arvel/shutdown; then it calls shutdown and stops the peer.
func (c *core) run(ctx context.Context, shutdown func()) error {
	if !c.o.stdio {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
	}
	serveCtx, cancelServe := context.WithCancel(context.Background())
	defer cancelServe()
	serveErr := make(chan error, 1)
	go func() { serveErr <- c.peer.Serve(serveCtx) }()

	var result error
	ended := false
	select {
	case <-ctx.Done():
	case <-c.shutdownReq:
	case err := <-serveErr:
		ended, result = true, err
	}
	shutdown()
	cancelServe()
	if !ended {
		<-serveErr // the error is the cancellation we caused
	}
	if errors.Is(result, context.Canceled) {
		result = nil
	}
	return result
}

// invalidResult logs a contract violation of the plugin and returns the error
// the host gets instead of the invalid result.
func (c *core) invalidResult(what string, err error) error {
	c.log.Error("plugin returned an invalid "+what, "err", err.Error())
	return protocol.NewError(protocol.ErrInternal, "plugin returned an invalid "+what, nil)
}

func (c *core) rpcError(err error) error {
	if re := toRPCError(c.redactor, c.log, err); re != nil {
		return re
	}
	return nil
}

// statusDebounce drops a status that is identical to the previous one.
type statusDebounce struct {
	mu   sync.Mutex
	last []byte
}

func (d *statusDebounce) Seed(st protocol.Status) {
	b, _ := json.Marshal(st)
	d.mu.Lock()
	d.last = b
	d.mu.Unlock()
}

func (d *statusDebounce) Changed(st protocol.Status) bool {
	b, _ := json.Marshal(st)
	d.mu.Lock()
	defer d.mu.Unlock()
	if bytes.Equal(b, d.last) {
		return false
	}
	d.last = b
	return true
}

// redactStatus scrubs the free-text fields of a status.
func redactStatus(rd *redact.Redactor, st protocol.Status) protocol.Status {
	st.Message = rd.String(st.Message)
	st.BillingNote = rd.String(st.BillingNote)
	return st
}
