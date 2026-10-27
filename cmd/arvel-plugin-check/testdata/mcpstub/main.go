// SPDX-License-Identifier: Apache-2.0

// Command mcpstub is a test-only MCP stdio server that answers initialize and
// tools/list with two fixed tools. With STUB_MCP_MODE=silent it never answers,
// for the deadline test. It is built and driven by the arvel-plugin-check
// tests; it is not shipped.
package main

import (
	"bufio"
	"encoding/json"
	"os"
)

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
}

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		var req request
		if err := json.Unmarshal(in.Bytes(), &req); err != nil || len(req.ID) == 0 {
			continue // a notification or a malformed line: nothing to answer
		}
		if os.Getenv("STUB_MCP_MODE") == "silent" {
			continue // never answer: the caller's deadline must fire
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "mcpstub", "version": "0"},
			}
		case "tools/list":
			result = map[string]any{
				"tools": []map[string]any{
					{"name": "alpha", "inputSchema": map[string]any{"type": "object"}},
					{"name": "beta", "inputSchema": map[string]any{"type": "object"}},
				},
			}
		default:
			_ = out.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32601, "message": "method not found"},
			})
			continue
		}
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
}
