// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"log/slog"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// host is the Host handed to a provider.
type host struct{ s *providerServer }

func (h host) StatusChanged(st protocol.Status) {
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

func (h host) UsageUpdated(u protocol.Usage) {
	s := h.s
	if !s.inited.Load() {
		s.log.Debug("usage update before initialize dropped")
		return
	}
	if err := u.Validate(); err != nil {
		s.log.Warn("invalid usage update dropped", "err", err.Error())
		return
	}
	s.usage.Update(u)
}

func (h host) Logger() *slog.Logger { return h.s.log }
