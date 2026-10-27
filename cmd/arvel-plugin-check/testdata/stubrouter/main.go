// SPDX-License-Identifier: Apache-2.0

// Command stubrouter is a test-only router that fails exactly one conformance
// check, selected by the STUB_MODE environment variable. It speaks raw JSON-RPC
// over stdio (it is deliberately not built on the SDK) so the checker can be
// proven against a router that does not self-validate its results. It is not
// shipped.
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

func main() {
	mode := os.Getenv("STUB_MODE")
	enc := json.NewEncoder(os.Stdout)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			continue
		}
		if len(msg.ID) == 0 || string(msg.ID) == "null" {
			if msg.Method == "_arvel/shutdown" {
				return
			}
			continue
		}
		switch msg.Method {
		case "initialize":
			enc.Encode(initializeResponse(msg.ID, mode))
		case "_arvel/status":
			enc.Encode(ok(msg.ID, map[string]any{"state": "ready"}))
		case "session/new":
			if mode == "session-new" {
				enc.Encode(ok(msg.ID, map[string]any{"sessionId": "s1"}))
			} else {
				enc.Encode(rpcErr(msg.ID, -32601, "method not found"))
			}
		case "_arvel/route/plan":
			switch mode {
			case "confidence":
				enc.Encode(ok(msg.ID, map[string]any{
					"type":  nil,
					"roles": []map[string]any{{"slot": "developer", "include": true, "confidence": 1.5}},
				}))
			case "stall":
				time.Sleep(30 * time.Second) // the checker's deadline fires first
				enc.Encode(ok(msg.ID, map[string]any{"type": nil, "roles": []any{}}))
			default:
				enc.Encode(ok(msg.ID, map[string]any{"type": nil, "roles": []any{}}))
			}
		case "_arvel/route/message":
			enc.Encode(ok(msg.ID, map[string]any{"roleId": firstCandidate(msg.Params), "confidence": 0.5}))
		case "_arvel/route/effort":
			enc.Encode(ok(msg.ID, map[string]any{"effort": firstLevel(msg.Params), "confidence": 0.5}))
		case "_arvel/route/explain":
			enc.Encode(ok(msg.ID, map[string]any{"lines": []map[string]any{{"text": "ok", "value": 0.5}}}))
		default:
			enc.Encode(rpcErr(msg.ID, -32601, "method not found"))
		}
	}
}

// initializeResponse returns a conformant initialize result, except in
// "agent-capability" mode where agentCapabilities is non-empty.
func initializeResponse(id json.RawMessage, mode string) map[string]any {
	caps := map[string]any{}
	if mode == "agent-capability" {
		caps = map[string]any{"loadSession": true}
	}
	return ok(id, map[string]any{
		"protocolVersion":   1,
		"agentCapabilities": caps,
		"authMethods":       []any{},
		"_meta": map[string]any{
			"arvel": map[string]any{"pluginApi": "1", "features": []string{"route.plan"}},
		},
	})
}

// firstCandidate returns the first candidate roleId of a route/message request,
// falling back to "developer".
func firstCandidate(raw json.RawMessage) string {
	var p struct {
		Candidates []struct {
			RoleID string `json:"roleId"`
		} `json:"candidates"`
	}
	if json.Unmarshal(raw, &p) == nil && len(p.Candidates) > 0 && p.Candidates[0].RoleID != "" {
		return p.Candidates[0].RoleID
	}
	return "developer"
}

// firstLevel returns the first level of a route/effort request, falling back to
// "low".
func firstLevel(raw json.RawMessage) string {
	var p struct {
		Levels []string `json:"levels"`
	}
	if json.Unmarshal(raw, &p) == nil && len(p.Levels) > 0 && p.Levels[0] != "" {
		return p.Levels[0]
	}
	return "low"
}

func ok(id json.RawMessage, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func rpcErr(id json.RawMessage, code int, msg string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}}
}
