// SPDX-License-Identifier: Apache-2.0

// Command strictprovider exercises host inputs without external services.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/arvel-dev/arvel-sdk/plugin"
	"github.com/arvel-dev/arvel-sdk/protocol"
	acp "github.com/coder/acp-go-sdk"
)

type provider struct {
	executable, proxy, mode  string
	dataDir, auditPath, hold string
	next, prompts            atomic.Int64
	overlap                  chan struct{}
	once                     sync.Once
}

// router uses the same strict initialization while exposing no provider sessions.
type router struct{ p *provider }

func (r *router) Initialize(ctx context.Context, in protocol.InitMeta) (plugin.RouterFeatures, error) {
	_, err := r.p.Initialize(ctx, in)
	return plugin.RouterFeatures{}, err
}
func (r *router) Status(ctx context.Context) (protocol.Status, error) { return r.p.Status(ctx) }
func (r *router) Shutdown(ctx context.Context) error                  { return r.p.Shutdown(ctx) }
func (r *router) Plan(context.Context, protocol.RoutePlanParams) (protocol.RoutePlanResult, error) {
	return protocol.RoutePlanResult{}, nil
}

func directory(path string) bool {
	if !filepath.IsAbs(path) || filepath.Dir(path) == path {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.IsDir() && (runtime.GOOS == "windows" || st.Mode().Perm()&0077 == 0)
}

func (p *provider) Initialize(_ context.Context, in protocol.InitMeta) (plugin.Features, error) {
	if in.Connection.Kind != "synthetic" {
		return plugin.Features{}, errors.New("connection.kind: undeclared")
	}
	if !directory(in.DataDir) {
		return plugin.Features{}, errors.New("dataDir: private absolute directory required")
	}
	path, ok := in.Connection.Config["executable"].(string)
	if !ok || !filepath.IsAbs(path) {
		return plugin.Features{}, errors.New("connection.config.executable: absolute executable required")
	}
	if enabled, ok := in.Connection.Config["enabled"].(bool); !ok || enabled {
		return plugin.Features{}, errors.New("connection.config.enabled: false required")
	}
	if count, ok := in.Connection.Config["count"].(float64); !ok || count != 2 {
		return plugin.Features{}, errors.New("connection.config.count: integer required")
	}
	if in.Connection.Secrets["token"] == "" {
		return plugin.Features{}, errors.New("connection.secrets.token: required")
	}
	p.executable = path
	p.dataDir = in.DataDir
	p.hold, _ = in.Connection.Config["holdExecutable"].(string)
	p.audit("initialize", "", 0)
	if in.Sandbox != nil {
		p.proxy = in.Sandbox.Proxy
	}
	if p.mode == "leak" {
		fmt.Fprintln(os.Stderr, in.Connection.Secrets["token"])
	}
	if p.mode == "leak-canary" {
		fmt.Fprintln(os.Stderr, in.Connection.Secrets["conformance-canary"])
	}
	return plugin.Features{LoadSession: p.mode != "reject-resume"}, nil
}
func (p *provider) Status(ctx context.Context) (protocol.Status, error) {
	if p.mode == "not-ready" {
		return protocol.Status{State: protocol.StateNeedsLogin}, nil
	}
	cmd := exec.CommandContext(ctx, p.executable)
	if err := cmd.Start(); err != nil {
		return protocol.Status{}, errors.New("fixture dependency start failed")
	}
	p.audit("dependency", "", cmd.Process.Pid)
	if err := cmd.Wait(); err != nil {
		return protocol.Status{}, errors.New("fixture dependency failed")
	}
	return protocol.Status{State: protocol.StateReady}, nil
}
func (p *provider) Models(context.Context, bool) (protocol.ModelsResult, error) {
	return protocol.ModelsResult{Models: []protocol.Model{{ID: "fixture", Label: "Fixture"}}, Default: "fixture"}, nil
}
func (p *provider) Usage(context.Context) (protocol.Usage, error) {
	return protocol.Usage{Source: protocol.UsageSourceUnknown, Status: protocol.UsageStatusOK}, nil
}
func (p *provider) Action(context.Context, string) (protocol.ActionResult, error) {
	return protocol.ActionResult{Kind: protocol.ActionKindDone}, nil
}
func (p *provider) NewSession(_ context.Context, in plugin.SessionParams) (plugin.Session, error) {
	if !directory(in.CWD) {
		return nil, errors.New("cwd: private absolute workspace required")
	}
	p.audit("session", in.CWD, 0)
	return &session{p: p, cwd: in.CWD, id: fmt.Sprintf("s%d", p.next.Add(1)), cancel: make(chan struct{})}, nil
}
func (p *provider) LoadSession(ctx context.Context, in plugin.LoadParams) (plugin.Session, error) {
	if p.mode == "reject-resume" {
		return nil, &protocol.RPCError{Code: protocol.CodeMethodNotFound, Message: "unsupported"}
	}
	return p.NewSession(ctx, in.SessionParams)
}
func (p *provider) Shutdown(context.Context) error { p.audit("shutdown", "", 0); return nil }

func (p *provider) audit(event, cwd string, child int) {
	if p.auditPath == "" {
		return
	}
	data, _ := json.Marshal(map[string]any{"event": event, "dataDir": p.dataDir, "cwd": cwd, "executable": p.executable, "child": child})
	f, err := os.OpenFile(p.auditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

type session struct {
	p      *provider
	id     string
	cwd    string
	cancel chan struct{}
	once   sync.Once
}

func (s *session) ID() string                  { return s.id }
func (s *session) Cancel()                     { s.once.Do(func() { close(s.cancel) }) }
func (s *session) Close(context.Context) error { s.Cancel(); return nil }
func (*session) SetOption(context.Context, string, string) ([]acp.SessionConfigOption, error) {
	return nil, nil
}
func (s *session) Prompt(ctx context.Context, _ []acp.ContentBlock, out plugin.Output) (plugin.TurnResult, error) {
	if s.p.mode != "hold" {
		if err := out.Update(acp.UpdateAgentMessageText("started"), nil); err != nil {
			return plugin.TurnResult{}, err
		}
	}
	switch s.p.mode {
	case "interleave":
		n := s.p.prompts.Add(1)
		if n == 2 {
			s.p.once.Do(func() { close(s.p.overlap) })
		}
		if n <= 2 {
			select {
			case <-s.p.overlap:
			case <-ctx.Done():
				return plugin.TurnResult{}, ctx.Err()
			case <-time.After(3 * time.Second):
				return plugin.TurnResult{}, errors.New("sessions did not overlap")
			}
		}
	case "hold":
		var done chan error
		stop := func() {}
		if s.p.hold != "" {
			childCtx, cancel := context.WithCancel(ctx)
			stop = cancel
			defer cancel()
			cmd := exec.CommandContext(childCtx, s.p.hold)
			cmd.Dir = s.cwd
			if err := cmd.Start(); err != nil {
				return plugin.TurnResult{}, errors.New("hold dependency start failed")
			}
			s.p.audit("hold", s.cwd, cmd.Process.Pid)
			done = make(chan error, 1)
			go func() { done <- cmd.Wait() }()
		}
		if err := out.Update(acp.UpdateAgentMessageText("started"), nil); err != nil {
			stop()
			if done != nil {
				<-done
			}
			return plugin.TurnResult{}, err
		}
		select {
		case <-s.cancel:
		case <-ctx.Done():
		case <-done:
			return plugin.TurnResult{}, errors.New("hold dependency finished before cancellation")
		}
		stop()
		if done != nil {
			<-done
		}
		return plugin.TurnResult{StopReason: acp.StopReasonCancelled}, nil
	case "proxy", "bad-proxy":
		cmd := exec.CommandContext(ctx, s.p.executable)
		proxy := s.p.proxy
		if s.p.mode == "bad-proxy" {
			proxy = ""
		}
		cmd.Env = append(os.Environ(), "HTTPS_PROXY="+proxy, "HTTP_PROXY="+proxy, "ALL_PROXY="+proxy, "NO_PROXY=localhost,127.0.0.1")
		b, err := cmd.Output()
		if err != nil {
			return plugin.TurnResult{}, errors.New("fixture child failed")
		}
		for _, line := range strings.Split(string(b), "\n") {
			if line != "" {
				if err := out.Update(acp.UpdateAgentMessageText(line), nil); err != nil {
					return plugin.TurnResult{}, err
				}
			}
		}
	}
	return plugin.TurnResult{StopReason: acp.StopReasonEndTurn}, nil
}
func main() {
	p := &provider{mode: os.Getenv("FIXTURE_MODE"), auditPath: os.Getenv("FIXTURE_AUDIT"), overlap: make(chan struct{})}
	if os.Getenv("FIXTURE_START_REQUIRED") != "" && os.Getenv("FIXTURE_START") != "prepared" {
		os.Exit(2)
	}
	if os.Getenv("FIXTURE_TYPE") == "router" {
		if plugin.RunRouter(context.Background(), &router{p: p}, plugin.WithVersion("strict-router", "0.0.0")) != nil {
			os.Exit(1)
		}
		return
	}
	opts := []plugin.Option{plugin.WithVersion("strictprovider", "0.0.0")}
	if p.mode == "reject-resume" {
		opts = append(opts, plugin.WithStdio(os.Stdin, resumeWriter{os.Stdout}))
	}
	if err := plugin.Run(context.Background(), p, opts...); err != nil {
		os.Exit(1)
	}
}

// resumeWriter deliberately advertises a capability the defective fixture
// refuses. This tests checker validation, without weakening normal fixture input.
type resumeWriter struct{ io.Writer }

func (w resumeWriter) Write(b []byte) (int, error) {
	var message map[string]any
	if json.Unmarshal(b, &message) == nil {
		if result, ok := message["result"].(map[string]any); ok && result["agentCapabilities"] != nil {
			meta := result["_meta"].(map[string]any)["arvel"].(map[string]any)
			meta["features"] = append(meta["features"].([]any), protocol.FeatureResume)
			result["agentCapabilities"].(map[string]any)["loadSession"] = true
			updated, err := json.Marshal(message)
			if err != nil {
				return 0, err
			}
			if _, err = w.Writer.Write(append(updated, '\n')); err != nil {
				return 0, err
			}
			return len(b), nil
		}
	}
	return w.Writer.Write(b)
}
