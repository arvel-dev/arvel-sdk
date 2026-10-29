// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/arvel-dev/arvel-sdk/jsonrpc"
	"github.com/arvel-dev/arvel-sdk/procx"
)

// mcpTimeout bounds each MCP request when the caller's context has no deadline.
// It is a variable so tests can shorten the slow paths.
var mcpTimeout = 10 * time.Second

// listTools starts a stdio MCP server, performs the MCP handshake (initialize,
// notifications/initialized) and returns the tool names from tools/list. The
// caller's context bounds the whole exchange; when it carries no deadline, each
// request gets mcpTimeout.
func listTools(ctx context.Context, command []string, dir string) ([]string, error) {
	if len(command) == 0 {
		return nil, errors.New("mcp: empty command")
	}
	path, err := resolveCommand(command[0], dir)
	if err != nil {
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, mcpTimeout)
		defer cancel()
	}

	runCtx, stopProcess := context.WithCancel(ctx)
	defer stopProcess()
	ctx = runCtx

	c2sR, c2sW := io.Pipe() // checker -> server stdin
	s2cR, s2cW := io.Pipe() // server stdout -> checker
	proc, err := procx.Start(ctx, procx.Spec{
		Path:   path,
		Args:   command[1:],
		Dir:    dir,
		Env:    os.Environ(),
		Stdin:  c2sR,
		Stdout: s2cW,
	})
	if err != nil {
		_ = c2sR.Close()
		_ = c2sW.Close()
		_ = s2cR.Close()
		_ = s2cW.Close()
		return nil, fmt.Errorf("mcp: start %s: %w", filepath.Base(path), err)
	}

	peer := jsonrpc.NewPeer(s2cR, c2sW)
	peerDone := make(chan struct{})
	go func() { defer close(peerDone); _ = peer.Serve(ctx) }()
	defer func() {
		_ = c2sW.Close()
		stopProcess()
		proc.KillTree(0)
		_ = s2cW.Close()
		_, _ = proc.Wait()
		<-peerDone
		_ = c2sR.Close()
		_ = s2cR.Close()
	}()

	if err := peer.Call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "arvel-plugin-check", "version": "0"},
	}, nil); err != nil {
		return nil, fmt.Errorf("mcp: initialize: %w", err)
	}
	if err := peer.Notify("notifications/initialized", nil); err != nil {
		return nil, fmt.Errorf("mcp: initialized notification: %w", err)
	}
	var res struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := peer.Call(ctx, "tools/list", map[string]any{}, &res); err != nil {
		return nil, fmt.Errorf("mcp: tools/list: %w", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, t := range res.Tools {
		names = append(names, t.Name)
	}
	return names, nil
}

// resolveCommand turns the program name of an MCP server spec into an absolute
// path. An absolute name passes through; a relative one is looked up in the
// package directory first and on PATH second (procx starts only absolute paths
// and never searches PATH itself).
func resolveCommand(name, dir string) (string, error) {
	if filepath.IsAbs(name) {
		return name, nil
	}
	if dir != "" {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return filepath.Abs(p)
		}
	}
	found, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("mcp: resolve %q: %w", name, err)
	}
	return filepath.Abs(found)
}
