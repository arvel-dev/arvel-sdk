// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/internal/leakcheck"
	"github.com/arvel-dev/arvel-sdk/plugin"
)

// syncBuffer is a goroutine-safe byte buffer for a plugin's stderr.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Host drives one plugin through the testing.TB surface: a provider or a
// router, in-process or as a binary. The protocol logic lives on the embedded
// core; Host only adds t.Fatalf / t.Cleanup behavior.
type Host struct {
	tb testing.TB
	*core
}

const defaultTimeout = 10 * time.Second

// InProcess starts a provider in-process and returns a Host that drives it.
func InProcess(t testing.TB, p plugin.Provider, opts ...plugin.Option) *Host {
	t.Helper()
	return startInProcess(t, func(ctx context.Context, o []plugin.Option) error {
		return plugin.Run(ctx, p, o...)
	}, opts)
}

// InProcessRouter starts a router in-process and returns a Host that drives it.
func InProcessRouter(t testing.TB, r plugin.Router, opts ...plugin.Option) *Host {
	t.Helper()
	return startInProcess(t, func(ctx context.Context, o []plugin.Option) error {
		return plugin.RunRouter(ctx, r, o...)
	}, opts)
}

func startInProcess(t testing.TB, run func(context.Context, []plugin.Option) error, opts []plugin.Option) *Host {
	t.Helper()
	h := &Host{tb: t, core: startCoreInProcess(run, opts)}
	t.Cleanup(func() {
		h.Close()
		leakcheck.Check(t, "plugin.(*providerServer)", "plugin.(*routerServer)", "plugin.(*core)",
			"plugin.(*throttle)", "jsonrpc.(*Peer)", "plugin.Run", "plugin.RunRouter")
	})
	return h
}

// startOptions configure Start.
type startOptions struct {
	args []string
	dir  string
	env  []string
}

// StartOption configures Start.
type StartOption func(*startOptions)

// WithArgs sets the arguments passed to the plugin binary.
func WithArgs(args ...string) StartOption {
	return func(o *startOptions) { o.args = args }
}

// WithDir sets the working directory of the plugin process.
func WithDir(dir string) StartOption {
	return func(o *startOptions) { o.dir = dir }
}

// WithEnv sets the environment of the plugin process. A nil env means the
// default minimal environment (see procx.Start).
func WithEnv(env []string) StartOption {
	return func(o *startOptions) { o.env = env }
}

// Start launches a plugin binary and returns a Host that drives it. It works
// for both provider and router binaries, which share the same stdio transport.
func Start(t testing.TB, binaryPath string, opts ...StartOption) *Host {
	t.Helper()
	c, err := startCore(binaryPath, opts...)
	if err != nil {
		t.Fatalf("mockhost: start %s: %v", binaryPath, err)
	}
	h := &Host{tb: t, core: c}
	t.Cleanup(func() { h.Close() })
	return h
}

// Shutdown sends _arvel/shutdown and waits for the plugin to exit, failing the
// test on error or timeout.
func (h *Host) Shutdown() {
	h.tb.Helper()
	if err := h.core.shutdown(); err != nil {
		h.tb.Fatalf("shutdown: %v", err)
	}
}
