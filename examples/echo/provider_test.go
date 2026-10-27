// SPDX-License-Identifier: MIT-0

package main

import (
	"context"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/manifest"
	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

func TestManifestIsValid(t *testing.T) {
	m, err := manifest.LoadFile("arvel.json")
	if err != nil {
		t.Fatal(err)
	}
	if ps := m.ValidateWith(manifest.Options{Official: true}); len(ps) != 0 {
		t.Fatal(ps)
	}
	if m.Type != manifest.TypeProvider {
		t.Fatalf("type = %s", m.Type)
	}
}

func TestProviderShape(t *testing.T) {
	p := newProvider()
	st, err := p.Status(context.Background())
	if err != nil || st.State != protocol.StateReady {
		t.Fatalf("status = %+v %v", st, err)
	}
	mo, err := p.Models(context.Background(), false)
	if err != nil || mo.Default != "echo-1" || len(mo.Models) != 1 {
		t.Fatalf("models = %+v %v", mo, err)
	}
	if got := mo.Models[0].Efforts; len(got) != 2 || got[0] != protocol.EffortLow || got[1] != protocol.EffortHigh {
		t.Fatalf("efforts = %v", got)
	}
	u, err := p.Usage(context.Background())
	if err != nil || u.Status != protocol.UsageStatusOK || len(u.Windows) != 1 {
		t.Fatalf("usage = %+v %v", u, err)
	}
	a, err := p.Action(context.Background(), "x")
	if err != nil || a.Kind != protocol.ActionKindDone {
		t.Fatalf("action = %+v %v", a, err)
	}
}

func TestProvider_Resume(t *testing.T) {
	p := newProvider()
	s1, err := p.NewSession(context.Background(), plugin.SessionParams{})
	if err != nil {
		t.Fatal(err)
	}
	id := s1.ID()
	s2, err := p.LoadSession(context.Background(), plugin.LoadParams{SessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	if s2.ID() != id {
		t.Fatalf("resumed id = %q, want %q", s2.ID(), id)
	}
}

type fakeOutput struct {
	updates []acp.SessionUpdate
	gate    chan struct{} // when set, Update blocks here after recording
	permFn  func(context.Context, plugin.PermissionRequest) (plugin.PermissionDecision, error)
	permN   int
}

func (o *fakeOutput) Update(u acp.SessionUpdate, _ *protocol.UpdateMeta) error {
	o.updates = append(o.updates, u)
	if o.gate != nil {
		<-o.gate
	}
	return nil
}

func (o *fakeOutput) RequestPermission(ctx context.Context, req plugin.PermissionRequest) (plugin.PermissionDecision, error) {
	o.permN++
	if o.permFn != nil {
		return o.permFn(ctx, req)
	}
	return plugin.PermissionDecision{OptionID: "allow_once"}, nil
}

func textOf(u acp.SessionUpdate) string {
	if u.AgentMessageChunk != nil && u.AgentMessageChunk.Content.Text != nil {
		return u.AgentMessageChunk.Content.Text.Text
	}
	return ""
}

func TestSession_StreamsWordByWord(t *testing.T) {
	s := &session{id: "s_1"}
	o := &fakeOutput{}
	res, err := s.Prompt(context.Background(), []acp.ContentBlock{acp.TextBlock("hello world")}, o)
	if err != nil || res.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("prompt = %+v %v", res, err)
	}
	var got []string
	for _, u := range o.updates {
		got = append(got, strings.TrimSpace(textOf(u)))
	}
	want := []string{"hello", "world"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("streamed %q, want %q", got, want)
	}
}

func TestSession_PermissionOnBang(t *testing.T) {
	t.Run("allow", func(t *testing.T) {
		s := &session{id: "s_1"}
		o := &fakeOutput{}
		res, err := s.Prompt(context.Background(), []acp.ContentBlock{acp.TextBlock("please !run now")}, o)
		if err != nil || res.StopReason != acp.StopReasonEndTurn {
			t.Fatalf("prompt = %+v %v", res, err)
		}
		if o.permN != 1 {
			t.Fatalf("permission requests = %d", o.permN)
		}
		if last := textOf(o.updates[len(o.updates)-1]); last != "ran go test ./..." {
			t.Fatalf("last chunk = %q", last)
		}
	})
	t.Run("reject", func(t *testing.T) {
		s := &session{id: "s_1"}
		o := &fakeOutput{permFn: func(context.Context, plugin.PermissionRequest) (plugin.PermissionDecision, error) {
			return plugin.PermissionDecision{OptionID: "reject_once"}, nil
		}}
		res, err := s.Prompt(context.Background(), []acp.ContentBlock{acp.TextBlock("please !run now")}, o)
		if err != nil || res.StopReason != acp.StopReasonEndTurn {
			t.Fatalf("prompt = %+v %v", res, err)
		}
		if last := textOf(o.updates[len(o.updates)-1]); last != "denied" {
			t.Fatalf("last chunk = %q", last)
		}
	})
	t.Run("no bang", func(t *testing.T) {
		s := &session{id: "s_1"}
		o := &fakeOutput{}
		if _, err := s.Prompt(context.Background(), []acp.ContentBlock{acp.TextBlock("hello")}, o); err != nil {
			t.Fatal(err)
		}
		if o.permN != 0 {
			t.Fatalf("permission requests = %d", o.permN)
		}
	})
}

type blockingOutput struct {
	updates []acp.SessionUpdate
	entered chan struct{} // closed after the first Update
	release chan struct{} // Update blocks until this is closed
}

func (o *blockingOutput) Update(u acp.SessionUpdate, _ *protocol.UpdateMeta) error {
	o.updates = append(o.updates, u)
	select {
	case <-o.entered:
	default:
		close(o.entered)
	}
	<-o.release
	return nil
}

func (o *blockingOutput) RequestPermission(context.Context, plugin.PermissionRequest) (plugin.PermissionDecision, error) {
	return plugin.PermissionDecision{OptionID: "allow_once"}, nil
}

func TestSession_Cancel(t *testing.T) {
	s := &session{id: "s_1"}
	o := &blockingOutput{entered: make(chan struct{}), release: make(chan struct{})}
	type result struct {
		r   plugin.TurnResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		r, err := s.Prompt(context.Background(), []acp.ContentBlock{acp.TextBlock("one two three")}, o)
		done <- result{r, err}
	}()
	<-o.entered // the first word is emitted and the loop is now blocked
	s.Cancel()
	close(o.release)
	got := <-done
	if got.err != nil || got.r.StopReason != acp.StopReasonCancelled {
		t.Fatalf("prompt = %+v %v, want cancelled", got.r, got.err)
	}
}
