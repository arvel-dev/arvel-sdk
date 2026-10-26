// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

func (s *providerServer) registerMethods() {
	s.peer.Handle(protocol.MethodStatus, s.status)
	s.peer.Handle(protocol.MethodModels, s.models)
	s.peer.Handle(protocol.MethodUsage, s.usageCall)
	s.peer.Handle(protocol.MethodAction, s.action)
	if _, ok := s.p.(Discoverer); ok {
		s.peer.Handle(protocol.MethodDiscover, s.discover)
	}
}

func (s *providerServer) status(ctx context.Context, _ json.RawMessage) (any, error) {
	if err := s.requireInit(); err != nil {
		return nil, err
	}
	st, err := s.p.Status(ctx)
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

func (s *providerServer) models(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireInit(); err != nil {
		return nil, err
	}
	var params protocol.ModelsParams
	if err := decodeParams(raw, &params); err != nil {
		return nil, err
	}
	res, err := s.p.Models(ctx, params.Refresh)
	if err != nil {
		return nil, s.rpcError(err)
	}
	if err := res.Validate(); err != nil {
		return nil, s.invalidResult("models result", err)
	}
	return res, nil
}

func (s *providerServer) usageCall(ctx context.Context, _ json.RawMessage) (any, error) {
	if err := s.requireInit(); err != nil {
		return nil, err
	}
	u, err := s.p.Usage(ctx)
	if err != nil {
		return nil, s.rpcError(err)
	}
	if err := u.Validate(); err != nil {
		return nil, s.invalidResult("usage", err)
	}
	return u, nil
}

func (s *providerServer) action(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireInit(); err != nil {
		return nil, err
	}
	var params protocol.ActionParams
	if err := decodeParams(raw, &params); err != nil {
		return nil, err
	}
	if params.ID == "" {
		return nil, invalidParams("action id is required")
	}
	res, err := s.p.Action(ctx, params.ID)
	if err != nil {
		return nil, s.rpcError(err)
	}
	if err := res.Validate(); err != nil {
		return nil, s.invalidResult("action result", err)
	}
	return res, nil
}

func (s *providerServer) discover(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireInit(); err != nil {
		return nil, err
	}
	var params protocol.DiscoverParams
	if err := decodeParams(raw, &params); err != nil {
		return nil, err
	}
	res, err := s.p.(Discoverer).Discover(ctx, params)
	if err != nil {
		return nil, s.rpcError(err)
	}
	res = s.sanitizeDiscover(res)
	if err := res.Validate(); err != nil {
		return nil, s.invalidResult("discover result", err)
	}
	return res, nil
}

// sanitizeDiscover scrubs every string of a discover result and drops the env
// and header entries that are neither a secret marker nor an env reference.
// The types already keep literal values out; this removes malformed markers.
func (s *providerServer) sanitizeDiscover(r protocol.DiscoverResult) protocol.DiscoverResult {
	rd := s.redactor
	out := protocol.DiscoverResult{McpServers: []protocol.DiscoveredMcp{}, Skills: []protocol.DiscoveredSkill{}}
	for _, m := range r.McpServers {
		m.Name, m.Project, m.Source, m.URL = rd.String(m.Name), rd.String(m.Project), rd.String(m.Source), rd.String(m.URL)
		if m.Command != nil {
			cmd := make([]string, len(m.Command))
			for i, a := range m.Command {
				cmd[i] = rd.String(a)
			}
			m.Command = cmd
		}
		m.Env = s.cleanFields("env", m.Env)
		m.Headers = s.cleanFields("header", m.Headers)
		out.McpServers = append(out.McpServers, m)
	}
	for _, k := range r.Skills {
		k.Name, k.Source, k.Path = rd.String(k.Name), rd.String(k.Source), rd.String(k.Path)
		out.Skills = append(out.Skills, k)
	}
	return out
}

func (s *providerServer) cleanFields(kind string, in map[string]protocol.SecretField) map[string]protocol.SecretField {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]protocol.SecretField, len(in))
	for k, v := range in {
		if err := v.Validate(); err != nil {
			s.log.Warn("discover entry dropped", "kind", kind, "key", s.redactor.String(k))
			continue
		}
		out[s.redactor.String(k)] = protocol.SecretField{Secret: v.Secret, EnvRef: s.redactor.String(v.EnvRef)}
	}
	return out
}
