// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// Router is what a router plugin implements (PROTOCOL §14). The route.* features
// it announces are the optional interfaces it also implements: PlanRouter,
// MessageRouter, EffortRouter and ExplainRouter.
type Router interface {
	// Initialize is called once. It must return in under 5 s and must not use
	// the network.
	Initialize(ctx context.Context, in protocol.InitMeta) (RouterFeatures, error)
	// Status answers _arvel/status.
	Status(ctx context.Context) (protocol.Status, error)
	// Shutdown is called once at the end.
	Shutdown(ctx context.Context) error
}

// PlanRouter answers _arvel/route/plan.
type PlanRouter interface {
	Plan(ctx context.Context, p protocol.RoutePlanParams) (protocol.RoutePlanResult, error)
}

// MessageRouter answers _arvel/route/message.
type MessageRouter interface {
	Message(ctx context.Context, p protocol.RouteMessageParams) (protocol.RouteMessageResult, error)
}

// EffortRouter answers _arvel/route/effort.
type EffortRouter interface {
	Effort(ctx context.Context, p protocol.RouteEffortParams) (protocol.RouteEffortResult, error)
}

// ExplainRouter answers _arvel/route/explain.
type ExplainRouter interface {
	Explain(ctx context.Context, p protocol.RoutePlanParams) (protocol.RouteExplainResult, error)
}

// RouterFeatures is what Router.Initialize returns. The route.* features follow
// from the optional interfaces the router implements, so the struct is empty;
// fields can be added without breaking existing routers.
type RouterFeatures struct{}

// RouterHost is a router's handle for messages it starts itself.
type RouterHost interface {
	// StatusChanged sends _arvel/status_changed; identical consecutive statuses
	// are dropped.
	StatusChanged(protocol.Status)
	// Logger returns the plugin logger (JSON on stderr, secrets redacted).
	Logger() *slog.Logger
}

// RouterHostAware is implemented by a Router that wants its RouterHost.
// SetHost is called before Initialize.
type RouterHostAware interface{ SetHost(RouterHost) }

// ErrNoRouteFeature is returned by RunRouter for a router that implements none
// of PlanRouter, MessageRouter, EffortRouter and ExplainRouter.
var ErrNoRouteFeature = errors.New("plugin: router implements none of PlanRouter, MessageRouter, EffortRouter, ExplainRouter")

// routerServer is the ACP agent side of a Router.
type routerServer struct {
	*core
	r        Router
	features []string
	counters map[string]*routeCounter
}

// RunRouter serves a router plugin on stdin and stdout until the host sends
// _arvel/shutdown, stdin ends, a signal arrives or ctx is done. It answers
// initialize, _arvel/status, the route calls of the implemented interfaces and
// _arvel/shutdown; every other method, including every session/* method, gets
// -32601. Each route call runs under the route timeout, and its result is
// validated against its request before it is sent. Decisions the host ignores
// (auto slots, a type's lead_accept) are dropped and logged at warn.
func RunRouter(ctx context.Context, r Router, opts ...Option) error {
	if r == nil {
		return errors.New("plugin: nil router")
	}
	features := routerFeatureList(r)
	if len(features) == 0 {
		return ErrNoRouteFeature
	}
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}
	s := &routerServer{core: newCore(o), r: r, features: features, counters: map[string]*routeCounter{}}
	s.register()
	return s.core.run(ctx, s.shutdown)
}

func routerFeatureList(r Router) []string {
	var list []string
	if _, ok := r.(PlanRouter); ok {
		list = append(list, protocol.FeatureRoutePlan)
	}
	if _, ok := r.(MessageRouter); ok {
		list = append(list, protocol.FeatureRouteMessage)
	}
	if _, ok := r.(EffortRouter); ok {
		list = append(list, protocol.FeatureRouteEffort)
	}
	if _, ok := r.(ExplainRouter); ok {
		list = append(list, protocol.FeatureRouteExplain)
	}
	return list
}

func (s *routerServer) register() {
	s.registerCommon()
	s.peer.Handle(protocol.MethodInitialize, s.initialize)
	s.peer.Handle(protocol.MethodStatus, s.status)
	s.registerRoutes()
}

func (s *routerServer) initialize(ctx context.Context, raw json.RawMessage) (any, error) {
	s.initMu.Lock()
	defer s.initMu.Unlock()
	if s.inited.Load() {
		return nil, &protocol.RPCError{Code: protocol.CodeInvalidRequest, Message: "already initialized"}
	}
	in, err := s.decodeInit(raw)
	if err != nil {
		return nil, err
	}
	if h, ok := s.r.(RouterHostAware); ok {
		h.SetHost(routerHost{s})
	}
	ictx, cancel := context.WithTimeout(ctx, s.o.initTimeout)
	defer cancel()
	if _, err := s.r.Initialize(ictx, in); err != nil {
		s.log.Error("initialize failed", "err", err.Error())
		return nil, s.rpcError(err)
	}
	s.initMeta = in
	s.inited.Store(true)
	// agentCapabilities is exactly {} for a router (PROTOCOL §14.1).
	return s.initResult(struct{}{}, protocol.InitResultMeta{PluginAPI: protocol.PluginAPI, Features: s.features}), nil
}

func (s *routerServer) status(ctx context.Context, _ json.RawMessage) (any, error) {
	if err := s.requireInit(); err != nil {
		return nil, err
	}
	st, err := s.r.Status(ctx)
	if err != nil {
		return nil, s.rpcError(err)
	}
	if err := st.Validate(); err != nil {
		return nil, s.invalidResult("status", err)
	}
	st = redactStatus(s.redactor, st)
	s.debounce.Seed(st)
	return st, nil
}

// shutdown runs once: it shuts the router down, waiting at most the close timeout.
func (s *routerServer) shutdown() {
	s.shutOnce.Do(func() {
		s.closing.Store(true)
		if !s.inited.Load() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.o.closeTimeout)
		defer cancel()
		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = errors.New("router shutdown panicked")
				}
			}()
			return s.r.Shutdown(ctx)
		}()
		if err != nil {
			s.log.Error("router shutdown failed", "err", err.Error())
		}
	})
}

// routerHost is the RouterHost handed to a router.
type routerHost struct{ s *routerServer }

func (h routerHost) StatusChanged(st protocol.Status) {
	s := h.s
	if !s.inited.Load() {
		s.log.Debug("status change before initialize dropped")
		return
	}
	if err := st.Validate(); err != nil {
		s.log.Warn("invalid status change dropped", "err", err.Error())
		return
	}
	st = redactStatus(s.redactor, st)
	if !s.debounce.Changed(st) {
		return
	}
	if err := s.peer.Notify(protocol.MethodStatusChanged, st); err != nil {
		s.log.Debug("status change not sent", "err", err.Error())
	}
}

func (h routerHost) Logger() *slog.Logger { return h.s.log }
