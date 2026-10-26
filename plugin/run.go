// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// providerServer is the ACP agent side of a Provider.
type providerServer struct {
	*core
	p     Provider
	feats Features

	// ctx ends at shutdown; turns run under it.
	ctx         context.Context
	cancelTurns context.CancelFunc

	turnMu   sync.Mutex
	turnN    int           // prompts in flight
	turnIdle chan struct{} // closed when turnN reaches 0 while stopTurns waits
	stopping bool

	usage *throttle
	reg   *registry
	reap  reaper
}

// Run serves a provider plugin on stdin and stdout until the host sends
// _arvel/shutdown, stdin ends, a signal arrives (SIGINT, SIGTERM or a console
// close on Windows) or ctx is done. It then cancels the running turns, closes
// every session with a deadline, shuts the provider down and returns nil, so the
// process can exit 0. It returns an error only for invalid arguments or a
// failing transport.
func Run(ctx context.Context, p Provider, opts ...Option) error {
	if p == nil {
		return errors.New("plugin: nil provider")
	}
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}
	s := newProviderServer(newCore(o), p)
	s.register()
	return s.core.run(ctx, s.shutdown)
}

func newProviderServer(c *core, p Provider) *providerServer {
	s := &providerServer{core: c, p: p, reg: newRegistry()}
	s.ctx, s.cancelTurns = context.WithCancel(context.Background())
	s.usage = &throttle{clk: c.o.clock, interval: UsageInterval, send: func(u protocol.Usage) {
		if err := c.peer.Notify(protocol.MethodUsageUpdated, u); err != nil {
			c.log.Debug("usage update not sent", "err", err.Error())
		}
	}}
	return s
}

func (s *providerServer) register() {
	s.registerCommon()
	s.peer.Handle(protocol.MethodInitialize, s.initialize)
	s.registerMethods()
	s.registerSessions()
}

func (s *providerServer) initialize(ctx context.Context, raw json.RawMessage) (any, error) {
	s.initMu.Lock()
	defer s.initMu.Unlock()
	if s.inited.Load() {
		return nil, &protocol.RPCError{Code: protocol.CodeInvalidRequest, Message: "already initialized"}
	}
	in, err := s.decodeInit(raw)
	if err != nil {
		return nil, err
	}
	if h, ok := s.p.(HostAware); ok {
		h.SetHost(host{s})
	}
	ictx, cancel := context.WithTimeout(ctx, s.o.initTimeout)
	defer cancel()
	feats, err := s.p.Initialize(ictx, in)
	if err != nil {
		s.log.Error("initialize failed", "err", err.Error())
		return nil, s.rpcError(err)
	}
	s.feats, s.initMeta = feats, in
	s.inited.Store(true)
	s.startReaper()

	caps := agentCapabilities{LoadSession: feats.LoadSession}
	caps.PromptCapabilities.Image = feats.PromptImage
	caps.PromptCapabilities.Audio = feats.PromptAudio
	caps.PromptCapabilities.EmbeddedContext = feats.PromptEmbeddedContext
	caps.McpCapabilities.HTTP = feats.MCPHTTP
	caps.McpCapabilities.SSE = feats.MCPSSE
	_, discover := s.p.(Discoverer)
	return s.initResult(caps, protocol.InitResultMeta{
		PluginAPI:        protocol.PluginAPI,
		Features:         featureList(feats, discover),
		InstructionFiles: feats.InstructionFiles,
		Skills:           feats.Skills,
	}), nil
}

// agentCapabilities is the ACP agent capabilities object. It is an own type, not
// acp.AgentCapabilities, so every member is written, zero values included, as
// in the PROTOCOL.md example, and nothing else is added.
type agentCapabilities struct {
	LoadSession        bool `json:"loadSession"`
	PromptCapabilities struct {
		Image           bool `json:"image"`
		Audio           bool `json:"audio"`
		EmbeddedContext bool `json:"embeddedContext"`
	} `json:"promptCapabilities"`
	McpCapabilities struct {
		HTTP bool `json:"http"`
		SSE  bool `json:"sse"`
	} `json:"mcpCapabilities"`
}

// featureList is the "features" list of the initialize result, in protocol order.
func featureList(f Features, discover bool) []string {
	list := []string{protocol.FeatureStatus, protocol.FeatureModels, protocol.FeatureUsage, protocol.FeatureActions}
	if f.Subagents {
		list = append(list, protocol.FeatureSubagents)
	}
	if f.LoadSession {
		list = append(list, protocol.FeatureResume)
	}
	if f.Skills != nil {
		list = append(list, protocol.FeatureSkills)
	}
	if discover {
		list = append(list, protocol.FeatureDiscover)
	}
	if f.MaxParallelSessions > 0 {
		list = append(list, protocol.FeatureMaxParallelSessions(f.MaxParallelSessions))
	}
	return list
}

// shutdown runs once: it stops the reaper, cancels and closes the sessions,
// flushes the pending usage update and shuts the provider down. Nothing waits
// longer than the close timeout.
func (s *providerServer) shutdown() {
	s.shutOnce.Do(func() {
		s.closing.Store(true)
		if !s.inited.Load() {
			return
		}
		s.stopReaper()
		s.cancelAllSessions()
		s.stopTurns()
		s.closeSessions()
		s.usage.Flush()

		ctx, cancel := context.WithTimeout(context.Background(), s.o.closeTimeout)
		defer cancel()
		if err := s.shutdownProvider(ctx); err != nil {
			s.log.Error("provider shutdown failed", "err", err.Error())
		}
	})
}

func (s *providerServer) shutdownProvider(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("provider shutdown panicked")
		}
	}()
	return s.p.Shutdown(ctx)
}
