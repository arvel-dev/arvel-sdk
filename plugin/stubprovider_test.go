// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// stubProvider is a configurable Provider. Unset function fields give valid
// canned answers taken from the golden files of PROTOCOL.md.
type stubProvider struct {
	t     *testing.T
	feats Features

	initialize  func(ctx context.Context, in protocol.InitMeta) (Features, error)
	status      func(ctx context.Context) (protocol.Status, error)
	models      func(ctx context.Context, refresh bool) (protocol.ModelsResult, error)
	usage       func(ctx context.Context) (protocol.Usage, error)
	action      func(ctx context.Context, id string) (protocol.ActionResult, error)
	newSession  func(ctx context.Context, p SessionParams) (Session, error)
	loadSession func(ctx context.Context, p LoadParams) (Session, error)
	shutdown    func(ctx context.Context) error

	mu            sync.Mutex
	host          Host
	hostAtInit    bool
	lastInit      protocol.InitMeta
	lastModelsRef bool
	lastNew       SessionParams
	lastLoad      LoadParams
	sessions      []*stubSession
	nextID        int

	initN, newN, loadN, shutdownN atomic.Int32
}

func newStub(t *testing.T) *stubProvider { return &stubProvider{t: t} }

func (p *stubProvider) SetHost(h Host) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.host = h
}

func (p *stubProvider) getHost() Host {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.host
}

func (p *stubProvider) Initialize(ctx context.Context, in protocol.InitMeta) (Features, error) {
	p.initN.Add(1)
	p.mu.Lock()
	p.lastInit, p.hostAtInit = in, p.host != nil
	p.mu.Unlock()
	if p.initialize != nil {
		return p.initialize(ctx, in)
	}
	return p.feats, nil
}

func (p *stubProvider) Status(ctx context.Context) (protocol.Status, error) {
	if p.status != nil {
		return p.status(ctx)
	}
	return goldenAs[protocol.Status](p.t, "6.1-status-result.json", ""), nil
}

func (p *stubProvider) Models(ctx context.Context, refresh bool) (protocol.ModelsResult, error) {
	p.mu.Lock()
	p.lastModelsRef = refresh
	p.mu.Unlock()
	if p.models != nil {
		return p.models(ctx, refresh)
	}
	return goldenAs[protocol.ModelsResult](p.t, "6.2-models-result.json", ""), nil
}

func (p *stubProvider) Usage(ctx context.Context) (protocol.Usage, error) {
	if p.usage != nil {
		return p.usage(ctx)
	}
	return goldenAs[protocol.Usage](p.t, "6.3-usage.json", ""), nil
}

func (p *stubProvider) Action(ctx context.Context, id string) (protocol.ActionResult, error) {
	if p.action != nil {
		return p.action(ctx, id)
	}
	return goldenAs[protocol.ActionResult](p.t, "6.4-action-result-done.json", ""), nil
}

func (p *stubProvider) NewSession(ctx context.Context, sp SessionParams) (Session, error) {
	p.newN.Add(1)
	p.mu.Lock()
	p.lastNew = sp
	p.mu.Unlock()
	if p.newSession != nil {
		return p.newSession(ctx, sp)
	}
	return p.makeSession(""), nil
}

func (p *stubProvider) LoadSession(ctx context.Context, lp LoadParams) (Session, error) {
	p.loadN.Add(1)
	p.mu.Lock()
	p.lastLoad = lp
	p.mu.Unlock()
	if p.loadSession != nil {
		return p.loadSession(ctx, lp)
	}
	return p.makeSession(lp.SessionID), nil
}

func (p *stubProvider) Shutdown(ctx context.Context) error {
	p.shutdownN.Add(1)
	if p.shutdown != nil {
		return p.shutdown(ctx)
	}
	return nil
}

// makeSession creates a stub session; an empty id gets s-1, s-2, ...
func (p *stubProvider) makeSession(id string) *stubSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id == "" {
		p.nextID++
		id = fmt.Sprintf("s-%d", p.nextID)
	}
	s := &stubSession{id: id}
	p.sessions = append(p.sessions, s)
	return s
}

// session returns the i-th created stub session.
func (p *stubProvider) session(i int) *stubSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessions[i]
}

func (p *stubProvider) sessionCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sessions)
}

// stubSession is a configurable Session.
type stubSession struct {
	id        string
	prompt    func(ctx context.Context, blocks []acp.ContentBlock, out Output) (TurnResult, error)
	setOption func(ctx context.Context, id, value string) ([]acp.SessionConfigOption, error)
	onCancel  func()
	onClose   func(ctx context.Context) error
	options   []acp.SessionConfigOption // reported through ConfigOptioner when withOptions

	cancelN, closeN, promptN atomic.Int32
}

func (s *stubSession) ID() string { return s.id }

func (s *stubSession) Prompt(ctx context.Context, blocks []acp.ContentBlock, out Output) (TurnResult, error) {
	s.promptN.Add(1)
	if s.prompt != nil {
		return s.prompt(ctx, blocks, out)
	}
	return TurnResult{StopReason: acp.StopReasonEndTurn}, nil
}

func (s *stubSession) SetOption(ctx context.Context, id, value string) ([]acp.SessionConfigOption, error) {
	if s.setOption != nil {
		return s.setOption(ctx, id, value)
	}
	return nil, nil
}

func (s *stubSession) Cancel() {
	s.cancelN.Add(1)
	if s.onCancel != nil {
		s.onCancel()
	}
}

func (s *stubSession) Close(ctx context.Context) error {
	s.closeN.Add(1)
	if s.onClose != nil {
		return s.onClose(ctx)
	}
	return nil
}

// stubSessionWithOptions adds ConfigOptioner.
type stubSessionWithOptions struct{ *stubSession }

func (s stubSessionWithOptions) ConfigOptions() []acp.SessionConfigOption { return s.options }

// stubDiscoverer adds Discoverer to a stub provider.
type stubDiscoverer struct {
	*stubProvider
	discover func(ctx context.Context, p protocol.DiscoverParams) (protocol.DiscoverResult, error)
}

func (d stubDiscoverer) Discover(ctx context.Context, p protocol.DiscoverParams) (protocol.DiscoverResult, error) {
	return d.discover(ctx, p)
}

// blockUntil returns a channel-based gate for prompts.
func gate() (wait func(), release func()) {
	ch := make(chan struct{})
	var once sync.Once
	return func() { <-ch }, func() { once.Do(func() { close(ch) }) }
}
