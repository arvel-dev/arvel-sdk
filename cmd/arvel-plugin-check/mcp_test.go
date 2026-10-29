// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"github.com/arvel-dev/arvel-sdk/internal/leakcheck"
	"strings"
	"testing"
	"time"
)

func TestListTools_TwoTools(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/mcpstub", "mcpstub")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tools, err := listTools(ctx, []string{bin}, "")
	if err != nil {
		t.Fatalf("listTools: %v", err)
	}
	if len(tools) != 2 || tools[0] != "alpha" || tools[1] != "beta" {
		t.Fatalf("tools = %v, want [alpha beta]", tools)
	}
	// listTools must join its peer before returning, while caller ctx is alive.
	leakcheck.Check(t, "jsonrpc.(*Peer)")
}

// A server that never answers must fail when the caller's deadline fires, not
// hang forever.
func TestListTools_SilentStub(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub binary")
	}
	t.Setenv("STUB_MCP_MODE", "silent")
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/mcpstub", "mcpstub")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := listTools(ctx, []string{bin}, "")
	if err == nil {
		t.Fatal("listTools returned no error for a silent server")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestListTools_MissingBinary(t *testing.T) {
	_, err := listTools(context.Background(), []string{"/no/such/mcp-server"}, "")
	if err == nil || (!strings.Contains(err.Error(), "start") && !strings.Contains(err.Error(), "resolve")) {
		t.Fatalf("err = %v, want a process launch error", err)
	}
}
