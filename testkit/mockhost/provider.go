// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// PromptResult is the outcome of a session/prompt call.
type PromptResult struct {
	StopReason acp.StopReason
	Meta       protocol.TurnMeta
}

// Status answers _arvel/status.
func (h *Host) Status() (protocol.Status, error) {
	var res protocol.Status
	err := h.call(protocol.MethodStatus, nil, &res)
	return res, err
}

// Models answers _arvel/models.
func (h *Host) Models(refresh bool) (protocol.ModelsResult, error) {
	var res protocol.ModelsResult
	err := h.call(protocol.MethodModels, map[string]any{"refresh": refresh}, &res)
	return res, err
}

// Usage answers _arvel/usage.
func (h *Host) Usage() (protocol.Usage, error) {
	var res protocol.Usage
	err := h.call(protocol.MethodUsage, nil, &res)
	return res, err
}

// Action runs one _arvel/action.
func (h *Host) Action(id string) (protocol.ActionResult, error) {
	var res protocol.ActionResult
	err := h.call(protocol.MethodAction, protocol.ActionParams{ID: id}, &res)
	return res, err
}

// NewSession creates a session and returns its id.
func (h *Host) NewSession(meta protocol.SessionMeta) (string, error) {
	var res struct {
		SessionID string `json:"sessionId"`
	}
	err := h.call(protocol.MethodSessionNew, map[string]any{
		"cwd":        "/",
		"mcpServers": []acp.McpServer{},
		"_meta":      protocol.WithMeta(nil, meta),
	}, &res)
	return res.SessionID, err
}

// LoadSession resumes a session.
func (h *Host) LoadSession(id string, meta protocol.SessionMeta) error {
	return h.call(protocol.MethodSessionLoad, map[string]any{
		"sessionId":  id,
		"cwd":        "/",
		"mcpServers": []acp.McpServer{},
		"_meta":      protocol.WithMeta(nil, meta),
	}, nil)
}

// Prompt runs one turn and returns its stop reason and turn metadata.
func (h *Host) Prompt(id string, blocks []acp.ContentBlock) (PromptResult, error) {
	var raw struct {
		StopReason acp.StopReason `json:"stopReason"`
		Meta       map[string]any `json:"_meta"`
	}
	err := h.call(protocol.MethodSessionPrompt, map[string]any{"sessionId": id, "prompt": blocks}, &raw)
	if err != nil {
		return PromptResult{}, err
	}
	tm, _, err := protocol.MetaFrom[protocol.TurnMeta](raw.Meta)
	if err != nil {
		return PromptResult{}, err
	}
	return PromptResult{StopReason: raw.StopReason, Meta: tm}, nil
}

// SetConfigOption changes a session config option.
func (h *Host) SetConfigOption(id, configID, value string) error {
	return h.call(protocol.MethodSessionSetConfigOption, map[string]any{
		"sessionId": id,
		"configId":  configID,
		"value":     value,
	}, nil)
}

// Cancel sends session/cancel for a session.
func (h *Host) Cancel(id string) error {
	return h.peer.Notify(protocol.MethodSessionCancel, map[string]any{"sessionId": id})
}

// Updates returns the session/update objects collected for a session, in
// arrival order. Each map is the decoded ACP SessionUpdate ("update" field).
func (h *Host) Updates(id string) []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]map[string]any, len(h.updates[id]))
	copy(out, h.updates[id])
	return out
}
