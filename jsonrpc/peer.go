// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/arvel-dev/arvel-sdk/jsonl"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// HandlerFunc answers one request. The returned value is marshalled into the
// result; a returned *protocol.RPCError is sent as is.
type HandlerFunc func(ctx context.Context, params json.RawMessage) (result any, err error)

// NotificationFunc handles one notification.
type NotificationFunc func(ctx context.Context, params json.RawMessage)

// ErrClosed is returned by Call and Notify once the peer has stopped.
var ErrClosed = errors.New("jsonrpc: peer closed")

// ErrServing is returned by a second call to Serve.
var ErrServing = errors.New("jsonrpc: Serve already called")

const defaultDrain = 5 * time.Second

type config struct {
	log     *slog.Logger
	maxLine int
	drain   time.Duration
	keyFn   func(method string, params json.RawMessage) string
}

// Option configures a Peer.
type Option func(*config)

// WithLogger sets the logger; the default discards everything. The peer never
// logs params or results.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.log = l
		}
	}
}

// WithMaxLine sets the longest accepted incoming line; the default is
// jsonl.DefaultMaxLine (64 MiB).
func WithMaxLine(n int) Option { return func(c *config) { c.maxLine = n } }

// WithDrainTimeout sets how long Serve waits for running handlers after it
// stopped; the default is 5 s.
func WithDrainTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.drain = d
		}
	}
}

// WithNotificationKey sets the function that groups notifications into ordered
// queues. Notifications with the same key are handled one at a time in arrival
// order. The default key is params.sessionId, or "" when absent.
func WithNotificationKey(f func(method string, params json.RawMessage) string) Option {
	return func(c *config) {
		if f != nil {
			c.keyFn = f
		}
	}
}

func sessionKey(_ string, params json.RawMessage) string {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if len(params) == 0 || json.Unmarshal(params, &p) != nil {
		return ""
	}
	return p.SessionID
}

type inNotification struct{}

type callResult struct {
	result json.RawMessage
	err    error
}

type pendingCall struct {
	ch        chan callResult
	noBarrier bool
}

// Peer is one end of a line-delimited JSON-RPC 2.0 connection. See the package
// documentation for the concurrency and ordering contract.
type Peer struct {
	cfg    config
	sc     *jsonl.Scanner
	w      *jsonl.Writer
	nextID atomic.Int64

	mu         sync.Mutex
	handlers   map[string]HandlerFunc
	notifs     map[string]NotificationFunc
	pending    map[int64]*pendingCall
	closed     bool
	serving    bool
	baseCtx    context.Context
	baseCancel context.CancelFunc

	wg      sync.WaitGroup // request handlers and barrier deliveries
	running atomic.Int64
	nt      notifier
}

// NewPeer returns a Peer that reads lines from r and writes lines to w.
func NewPeer(r io.Reader, w io.Writer, opts ...Option) *Peer {
	cfg := config{
		log:     slog.New(slog.DiscardHandler),
		maxLine: jsonl.DefaultMaxLine,
		drain:   defaultDrain,
		keyFn:   sessionKey,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return &Peer{
		cfg:      cfg,
		sc:       jsonl.NewScanner(r, cfg.maxLine),
		w:        jsonl.NewWriter(w),
		handlers: map[string]HandlerFunc{},
		notifs:   map[string]NotificationFunc{},
		pending:  map[int64]*pendingCall{},
		nt:       notifier{queues: map[string][]*item{}, live: map[*item]struct{}{}},
	}
}

// Handle registers the handler of a request method, replacing any earlier one.
func (p *Peer) Handle(method string, h HandlerFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[method] = h
}

// HandleNotification registers the handler of a notification method.
func (p *Peer) HandleNotification(method string, h NotificationFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notifs[method] = h
}

// Call sends a request and waits for the response. Cancelling ctx stops the
// wait and writes nothing to the wire. An error response is returned as a
// *protocol.RPCError.
func (p *Peer) Call(ctx context.Context, method string, params, result any) error {
	id := p.nextID.Add(1)
	pc := &pendingCall{ch: make(chan callResult, 1), noBarrier: ctx.Value(inNotification{}) != nil}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrClosed
	}
	p.pending[id] = pc
	p.mu.Unlock()

	if err := p.w.WriteJSON(outRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		p.dropPending(id)
		return fmt.Errorf("jsonrpc: call %s: %w", method, err)
	}
	select {
	case r := <-pc.ch:
		if r.err != nil {
			return r.err
		}
		if result == nil || len(r.result) == 0 || string(r.result) == "null" {
			return nil
		}
		if err := json.Unmarshal(r.result, result); err != nil {
			return fmt.Errorf("jsonrpc: decode result of %s: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		p.dropPending(id)
		return ctx.Err()
	}
}

func (p *Peer) dropPending(id int64) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

// Notify sends a notification. It returns after the line has been written.
func (p *Peer) Notify(method string, params any) error {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if err := p.w.WriteJSON(outNotification{JSONRPC: "2.0", Method: method, Params: params}); err != nil {
		return fmt.Errorf("jsonrpc: notify %s: %w", method, err)
	}
	return nil
}

// Serve reads and dispatches messages until the input ends, fails or ctx is
// done. It returns nil at a clean end of input, the read error, or ctx.Err().
// Serve may be called once.
func (p *Peer) Serve(ctx context.Context) error {
	p.mu.Lock()
	if p.serving {
		p.mu.Unlock()
		return ErrServing
	}
	p.serving = true
	p.baseCtx, p.baseCancel = context.WithCancel(ctx)
	p.mu.Unlock()

	readDone := make(chan error, 1)
	go func() { readDone <- p.readLoop() }()

	var cause error
	select {
	case cause = <-readDone:
	case <-ctx.Done():
		cause = ctx.Err()
	}
	p.finish(cause)
	return cause
}

func (p *Peer) readLoop() error {
	for p.sc.Scan() {
		if errors.Is(p.sc.Err(), jsonl.ErrLineTooLong) {
			p.cfg.log.Warn("dropped over-long line", "bytes", p.sc.Discarded())
			p.writeErr(json.RawMessage("null"), invalidRequest("message too large"))
			continue
		}
		line := p.sc.Bytes()
		if len(line) == 0 {
			continue
		}
		m, k, rerr := decode(line)
		if rerr != nil {
			p.writeErr(replyID(m), rerr)
			continue
		}
		switch k {
		case kindRequest:
			p.dispatchRequest(m)
		case kindNotification:
			p.dispatchNotification(m)
		case kindResponse:
			p.completeCall(m)
		}
	}
	return p.sc.Err()
}

func (p *Peer) dispatchRequest(m message) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.wg.Add(1)
	h := p.handlers[m.Method]
	ctx := p.baseCtx
	p.mu.Unlock()

	p.running.Add(1)
	go func() {
		defer p.wg.Done()
		defer p.running.Add(-1)
		if h == nil {
			p.writeErr(m.ID, protocol.NewMethodNotFound(m.Method))
			return
		}
		res, rerr := p.invoke(ctx, m, h)
		if rerr != nil {
			p.writeErr(m.ID, rerr)
			return
		}
		p.writeOK(m.ID, res)
	}()
}

// invoke runs a request handler and turns every outcome into a result or an
// RPC error. A panic never escapes.
func (p *Peer) invoke(ctx context.Context, m message, h HandlerFunc) (res any, rerr *protocol.RPCError) {
	defer func() {
		if r := recover(); r != nil {
			p.cfg.log.Error("handler panic", "method", m.Method, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			res, rerr = nil, protocol.NewError(protocol.ErrInternal, "internal error", nil)
		}
	}()
	res, err := h(ctx, m.Params)
	if err == nil {
		return res, nil
	}
	var re *protocol.RPCError
	if errors.As(err, &re) && re != nil {
		return nil, re
	}
	p.cfg.log.Error("handler error", "method", m.Method, "err", err.Error())
	return nil, &protocol.RPCError{Code: protocol.CodeInternalError, Message: "internal error"}
}

func (p *Peer) writeOK(id json.RawMessage, res any) {
	raw, err := json.Marshal(res)
	if err != nil {
		p.cfg.log.Error("cannot encode result", "err", err.Error())
		p.writeErr(id, protocol.NewError(protocol.ErrInternal, "internal error", nil))
		return
	}
	err = p.w.WriteJSON(okResponse{JSONRPC: "2.0", ID: id, Result: raw})
	if errors.Is(err, jsonl.ErrLineTooLong) {
		p.cfg.log.Error("response too large", "bytes", len(raw))
		p.writeErr(id, protocol.NewError(protocol.ErrInternal, "response too large", nil))
		return
	}
	if err != nil {
		p.cfg.log.Warn("cannot write response", "err", err.Error())
	}
}

func (p *Peer) writeErr(id json.RawMessage, rerr *protocol.RPCError) {
	if err := p.w.WriteJSON(errResponse{JSONRPC: "2.0", ID: id, Error: rerr}); err != nil {
		p.cfg.log.Warn("cannot write error response", "err", err.Error())
	}
}

func (p *Peer) completeCall(m message) {
	id, err := strconv.ParseInt(string(m.ID), 10, 64)
	var pc *pendingCall
	if err == nil {
		p.mu.Lock()
		pc = p.pending[id]
		delete(p.pending, id)
		p.mu.Unlock()
	}
	if pc == nil {
		p.cfg.log.Debug("response without pending call")
		return
	}
	res := callResult{result: m.Result}
	if m.Error != nil {
		res.err = m.Error
	}
	if pc.noBarrier {
		pc.ch <- res
		return
	}
	waits := p.nt.snapshot()
	if len(waits) == 0 {
		pc.ch <- res
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		pc.ch <- callResult{err: ErrClosed}
		return
	}
	p.wg.Add(1)
	ctx := p.baseCtx
	p.mu.Unlock()
	go func() {
		defer p.wg.Done()
		for _, ch := range waits {
			select {
			case <-ch:
			case <-ctx.Done():
				pc.ch <- callResult{err: ErrClosed}
				return
			}
		}
		pc.ch <- res
	}()
}

// finish stops the peer: it fails pending calls, cancels the handlers and
// waits for them for at most the drain timeout.
func (p *Peer) finish(cause error) {
	p.mu.Lock()
	p.closed = true
	pend := p.pending
	p.pending = map[int64]*pendingCall{}
	cancel := p.baseCancel
	p.mu.Unlock()
	cancel()

	err := ErrClosed
	if cause != nil {
		err = fmt.Errorf("%w: %v", ErrClosed, cause)
	}
	for _, pc := range pend {
		pc.ch <- callResult{err: err}
	}

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		p.nt.wg.Wait()
		close(done)
	}()
	t := time.NewTimer(p.cfg.drain)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
		p.cfg.log.Warn("handlers still running after drain",
			"count", p.running.Load()+int64(p.nt.workers()))
	}
}
