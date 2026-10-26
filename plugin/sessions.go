// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

func (s *providerServer) registerSessions() {
	s.peer.Handle(protocol.MethodSessionNew, s.newSession)
	s.peer.Handle(protocol.MethodSessionLoad, s.loadSession)
	s.peer.Handle(protocol.MethodSessionPrompt, s.prompt)
	s.peer.Handle(protocol.MethodSessionSetConfigOption, s.setConfigOption)
	s.peer.HandleNotification(protocol.MethodSessionCancel, s.cancel)
}

func (s *providerServer) shuttingDown() error {
	return &protocol.RPCError{Code: protocol.CodeInvalidRequest, Message: "plugin is shutting down"}
}

// sessionParams decodes the Arvel metadata of a session request.
func sessionParams(cwd string, servers []acp.McpServer, meta map[string]any) (SessionParams, error) {
	m, _, err := protocol.MetaFrom[protocol.SessionMeta](meta)
	if err != nil {
		return SessionParams{}, invalidParams("%v", err)
	}
	if err := m.Validate(); err != nil {
		return SessionParams{}, invalidParams("%v", err)
	}
	return SessionParams{CWD: cwd, MCPServers: servers, Meta: m}, nil
}

func (s *providerServer) configOptions(sess Session) []acp.SessionConfigOption {
	co, ok := sess.(ConfigOptioner)
	if !ok {
		return nil
	}
	var opts []acp.SessionConfigOption
	s.safely("", "Session.ConfigOptions", func() { opts = co.ConfigOptions() })
	return opts
}

// addSession registers a session the provider returned; the id must be unique.
func (s *providerServer) addSession(sess Session, params SessionParams) (*sessionEntry, error) {
	if sess == nil || sess.ID() == "" {
		s.log.Error("provider returned an invalid session")
		return nil, protocol.NewError(protocol.ErrInternal, "provider returned an invalid session", nil)
	}
	e := &sessionEntry{id: sess.ID(), sess: sess, params: params, lastActive: s.o.clock.Now()}
	if err := s.reg.add(e); err != nil {
		s.log.Error("provider returned a duplicate session id", "session", e.id)
		s.closeOne(e)
		return nil, protocol.NewError(protocol.ErrInternal, "provider returned a duplicate session id", nil)
	}
	return e, nil
}

func (s *providerServer) newSession(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireInit(); err != nil {
		return nil, err
	}
	if s.closing.Load() {
		return nil, s.shuttingDown()
	}
	var req acp.NewSessionRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, invalidParams("%v", err)
	}
	params, err := sessionParams(req.Cwd, req.McpServers, req.Meta)
	if err != nil {
		return nil, err
	}
	sess, err := s.p.NewSession(ctx, params)
	if err != nil {
		return nil, s.rpcError(err)
	}
	e, err := s.addSession(sess, params)
	if err != nil {
		return nil, err
	}
	return acp.NewSessionResponse{SessionId: acp.SessionId(e.id), ConfigOptions: s.configOptions(sess)}, nil
}

func (s *providerServer) loadSession(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireInit(); err != nil {
		return nil, err
	}
	if !s.feats.LoadSession {
		return nil, protocol.NewMethodNotFound(protocol.MethodSessionLoad)
	}
	if s.closing.Load() {
		return nil, s.shuttingDown()
	}
	var req acp.LoadSessionRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, invalidParams("%v", err)
	}
	id := string(req.SessionId)
	if e, ok := s.reg.get(id); ok {
		return acp.LoadSessionResponse{ConfigOptions: s.configOptions(e.sess)}, nil
	}
	params, err := sessionParams(req.Cwd, req.McpServers, req.Meta)
	if err != nil {
		return nil, err
	}
	s.reg.dropTombstone(id)
	sess, err := s.p.LoadSession(ctx, LoadParams{SessionID: id, SessionParams: params})
	if err != nil {
		return nil, s.rpcError(err)
	}
	if sess != nil && sess.ID() != id {
		s.log.Error("provider loaded a different session", "session", id)
		s.closeOne(&sessionEntry{id: id, sess: sess})
		return nil, protocol.NewError(protocol.ErrInternal, "provider loaded a different session", nil)
	}
	e, err := s.addSession(sess, params)
	if err != nil {
		return nil, err
	}
	return acp.LoadSessionResponse{ConfigOptions: s.configOptions(e.sess)}, nil
}

func (s *providerServer) setConfigOption(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireInit(); err != nil {
		return nil, err
	}
	var req acp.SetSessionConfigOptionRequest
	if err := decodeParams(raw, &req); err != nil {
		return nil, err
	}
	if req.ValueId == nil {
		return nil, invalidParams("boolean options are not supported")
	}
	e, err := s.acquire(ctx, string(req.ValueId.SessionId))
	if err != nil {
		return nil, err
	}
	e.touch(s.o.clock.Now())
	opts, err := e.sess.SetOption(ctx, string(req.ValueId.ConfigId), string(req.ValueId.Value))
	if err != nil {
		return nil, s.rpcError(err)
	}
	if opts == nil {
		opts = []acp.SessionConfigOption{}
	}
	return acp.SetSessionConfigOptionResponse{ConfigOptions: opts}, nil
}

// safely runs f and logs a panic instead of letting it escape.
func (s *providerServer) safely(session, what string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("provider panic", "what", what, "session", session,
				"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	f()
}

// closeOne closes a session with the close deadline.
func (s *providerServer) closeOne(e *sessionEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), s.o.closeTimeout)
	defer cancel()
	s.closeWith(ctx, e)
}

func (s *providerServer) closeWith(ctx context.Context, e *sessionEntry) {
	s.safely(e.id, "Session.Close", func() {
		if err := e.sess.Close(ctx); err != nil {
			s.log.Warn("session close failed", "session", e.id, "err", s.redactor.String(err.Error()))
		}
	})
}

// cancelAllSessions asks every running turn to stop gracefully.
func (s *providerServer) cancelAllSessions() {
	for _, e := range s.reg.all() {
		e.requestCancel(s)
	}
}

// closeSessions closes every live session in parallel, waiting at most the
// close timeout.
func (s *providerServer) closeSessions() {
	entries := s.reg.drain()
	if len(entries) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.o.closeTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.closeWith(ctx, e)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		s.log.Warn("sessions still closing at shutdown")
	}
}

// beginTurn counts a running prompt; it fails once shutdown started.
func (s *providerServer) beginTurn() bool {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.stopping {
		return false
	}
	s.turnN++
	return true
}

func (s *providerServer) endTurn() {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	s.turnN--
	if s.turnN == 0 && s.turnIdle != nil {
		close(s.turnIdle)
		s.turnIdle = nil
	}
}

// stopTurns cancels the turn context and waits for the running prompts, at most
// the close timeout.
func (s *providerServer) stopTurns() {
	s.cancelTurns()
	s.turnMu.Lock()
	s.stopping = true
	if s.turnN == 0 {
		s.turnMu.Unlock()
		return
	}
	idle := make(chan struct{})
	s.turnIdle = idle
	n := s.turnN
	s.turnMu.Unlock()

	t := time.NewTimer(s.o.closeTimeout)
	defer t.Stop()
	select {
	case <-idle:
	case <-t.C:
		s.log.Warn("prompts still running at shutdown", "count", n)
	}
}
