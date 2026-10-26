// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"bytes"
	"encoding/json"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// message is any JSON-RPC 2.0 object read from the wire.
type message struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      json.RawMessage    `json:"id,omitempty"`
	Method  string             `json:"method,omitempty"`
	Params  json.RawMessage    `json:"params,omitempty"`
	Result  json.RawMessage    `json:"result,omitempty"`
	Error   *protocol.RPCError `json:"error,omitempty"`
}

type kind int

const (
	kindRequest kind = iota + 1
	kindNotification
	kindResponse
)

func invalidRequest(msg string) *protocol.RPCError {
	return &protocol.RPCError{Code: protocol.CodeInvalidRequest, Message: msg}
}

// decode classifies one line. On failure it returns the error to send back;
// the message may then be partially filled, which replyID uses to find an id.
func decode(line []byte) (message, kind, *protocol.RPCError) {
	var m message
	if !json.Valid(line) {
		return m, 0, &protocol.RPCError{Code: protocol.CodeParseError, Message: "parse error"}
	}
	if t := bytes.TrimLeft(line, " \t\r\n"); len(t) == 0 || t[0] != '{' {
		return m, 0, invalidRequest("invalid request: expected a JSON object")
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return message{}, 0, invalidRequest("invalid request: malformed members")
	}
	if m.JSONRPC != "2.0" {
		return m, 0, invalidRequest(`invalid request: jsonrpc must be "2.0"`)
	}
	if m.Method != "" {
		if len(m.Result) > 0 || m.Error != nil {
			return m, 0, invalidRequest("invalid request: method with result or error")
		}
		if len(m.ID) == 0 {
			return m, kindNotification, nil
		}
		if string(m.ID) == "null" {
			return m, 0, invalidRequest("invalid request: id must not be null")
		}
		if !validID(m.ID) {
			return m, 0, invalidRequest("invalid request: id must be a number or a string")
		}
		return m, kindRequest, nil
	}
	hasResult, hasError := len(m.Result) > 0, m.Error != nil
	if len(m.ID) == 0 || string(m.ID) == "null" || hasResult == hasError || !validID(m.ID) {
		return m, 0, invalidRequest("invalid request: not a request, notification or response")
	}
	return m, kindResponse, nil
}

func validID(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	c := raw[0]
	return c == '"' || c == '-' || (c >= '0' && c <= '9')
}

// replyID returns the id to put in an error reply for a line that failed to
// decode: the line's own id when it has a valid one, otherwise null.
func replyID(m message) json.RawMessage {
	if validID(m.ID) {
		return m.ID
	}
	return json.RawMessage("null")
}

type okResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
}

type errResponse struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      json.RawMessage    `json:"id"`
	Error   *protocol.RPCError `json:"error"`
}

type outRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type outNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
