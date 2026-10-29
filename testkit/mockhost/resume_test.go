// SPDX-License-Identifier: Apache-2.0
package mockhost

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/protocol"
	acp "github.com/coder/acp-go-sdk"
)

type restartProvider struct {
	*stubProvider
	initialize          protocol.InitMeta
	loaded              plugin.LoadParams
	newCalls, loadCalls int
	text                string
}

func (p *restartProvider) Initialize(_ context.Context, in protocol.InitMeta) (plugin.Features, error) {
	p.initialize = in
	return plugin.Features{LoadSession: true}, nil
}
func (p *restartProvider) session(id string) *stubSession {
	return &stubSession{id: id, prompt: func(_ context.Context, _ []acp.ContentBlock, out plugin.Output) (plugin.TurnResult, error) {
		if err := out.Update(acp.UpdateAgentMessageText(p.text), nil); err != nil {
			return plugin.TurnResult{}, err
		}
		return plugin.TurnResult{StopReason: acp.StopReasonEndTurn}, nil
	}}
}
func (p *restartProvider) NewSession(context.Context, plugin.SessionParams) (plugin.Session, error) {
	p.newCalls++
	return p.session("persisted"), nil
}
func (p *restartProvider) LoadSession(_ context.Context, in plugin.LoadParams) (plugin.Session, error) {
	p.loadCalls++
	p.loaded = in
	return p.session(in.SessionID), nil
}

func TestLoadSession_FreshServerDelegatesRecoveryAndKeepsEnvironment(t *testing.T) {
	env, err := NewEnvironment(EnvironmentOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	first := &restartProvider{stubProvider: streamingProvider(), text: "first generation"}
	h1 := InProcessWithEnvironment(t, first, env)
	if _, err := h1.Initialize(protocol.InitMeta{Connection: protocol.ConnectionInfo{ID: "connection", Kind: "local"}}); err != nil {
		t.Fatal(err)
	}
	meta := protocol.SessionMeta{Model: "m1", Permissions: &protocol.Permissions{Edit: protocol.PermissionLevelDeny}, Task: &protocol.TaskMeta{Number: 1, Code: "RESUME", Type: "review", ReadOnly: true}}
	id, err := h1.NewSession(meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h1.Prompt(id, []acp.ContentBlock{acp.TextBlock("first")}); err != nil {
		t.Fatal(err)
	}
	if err := h1.CloseWithError(); err != nil {
		t.Fatal(err)
	}
	second := &restartProvider{stubProvider: streamingProvider(), text: "second generation"}
	h2 := InProcessWithEnvironment(t, second, env)
	if _, err := h2.Initialize(protocol.InitMeta{Connection: protocol.ConnectionInfo{ID: "connection", Kind: "local"}}); err != nil {
		t.Fatal(err)
	}
	if err := h2.LoadSession(id, meta); err != nil {
		t.Fatal(err)
	}
	if second.newCalls != 0 || second.loadCalls != 1 || second.loaded.SessionID != id || second.loaded.CWD != env.Workspace() {
		t.Fatal("fresh server did not delegate recovery", second.newCalls, second.loadCalls, second.loaded)
	}
	if first.initialize.DataDir != second.initialize.DataDir || !reflect.DeepEqual(first.initialize.Connection, second.initialize.Connection) {
		t.Fatal("recovery environment changed")
	}
	if second.loaded.Meta.Permissions.Edit != protocol.PermissionLevelDeny || !second.loaded.Meta.Task.ReadOnly || second.loaded.Meta.Model != "m1" {
		t.Fatal("load policy changed", second.loaded.Meta)
	}
	if len(h2.Updates(id)) != 0 {
		t.Fatal("previous server feed replayed as a live response")
	}
	if result, err := h2.Prompt(id, []acp.ContentBlock{acp.TextBlock("next")}); err != nil || result.StopReason != acp.StopReasonEndTurn {
		t.Fatal(result, err)
	}
	updates := h2.Updates(id)
	if len(updates) != 1 {
		t.Fatal("unexpected live updates", updates)
	}
	raw, err := json.Marshal(updates[0])
	if err != nil {
		t.Fatal(err)
	}
	var update struct {
		SessionUpdate string
		Content       struct{ Text string }
	}
	if err := json.Unmarshal(raw, &update); err != nil {
		t.Fatal(err)
	}
	if update.SessionUpdate != "agent_message_chunk" || update.Content.Text != "second generation" {
		t.Fatal("new prompt received old output", updates)
	}
	if err := h2.CloseWithError(); err != nil {
		t.Fatal(err)
	}
}
