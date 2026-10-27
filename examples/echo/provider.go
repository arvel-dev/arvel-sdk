// SPDX-License-Identifier: MIT-0

package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// provider implements plugin.Provider. It keeps every session in memory, so
// resume works for the lifetime of the process.
type provider struct {
	mu       sync.Mutex
	sessions map[string]*session
	next     atomic.Int64
}

func newProvider() *provider {
	return &provider{sessions: map[string]*session{}}
}

func (p *provider) Initialize(context.Context, protocol.InitMeta) (plugin.Features, error) {
	return plugin.Features{LoadSession: true}, nil
}

func (p *provider) Status(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateReady, Message: "Local echo"}, nil
}

func (p *provider) Models(context.Context, bool) (protocol.ModelsResult, error) {
	return protocol.ModelsResult{
		Models: []protocol.Model{{
			ID:            "echo-1",
			Label:         "Echo 1",
			Efforts:       []protocol.Effort{protocol.EffortLow, protocol.EffortHigh},
			DefaultEffort: protocol.EffortLow,
			Billing:       protocol.BillingUnknown,
			Hidden:        false,
		}},
		Default: "echo-1",
	}, nil
}

func (p *provider) Usage(context.Context) (protocol.Usage, error) {
	return protocol.Usage{
		Source:  protocol.UsageSourceUnknown,
		Status:  protocol.UsageStatusOK,
		Windows: []protocol.UsageWindow{{ID: "five_hour", Label: "5-hour window", UsedPercent: 38, DurationMinutes: 300}},
	}, nil
}

func (p *provider) Action(context.Context, string) (protocol.ActionResult, error) {
	return protocol.ActionResult{Kind: protocol.ActionKindDone, Message: "done"}, nil
}

func (p *provider) NewSession(context.Context, plugin.SessionParams) (plugin.Session, error) {
	s := &session{id: fmt.Sprintf("s_%d", p.next.Add(1))}
	p.mu.Lock()
	p.sessions[s.id] = s
	p.mu.Unlock()
	return s, nil
}

// LoadSession returns the stored session, or a fresh one with the same id if it
// was never created (the host keeps its own feed, so echo replays nothing).
func (p *provider) LoadSession(_ context.Context, lp plugin.LoadParams) (plugin.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.sessions[lp.SessionID]; ok {
		return s, nil
	}
	s := &session{id: lp.SessionID}
	p.sessions[s.id] = s
	return s, nil
}

func (p *provider) Shutdown(context.Context) error { return nil }
