// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"testing"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		kind kind
		code int // expected error code, 0 for success
	}{
		{"request_number_id", `{"jsonrpc":"2.0","id":1,"method":"m"}`, kindRequest, 0},
		{"request_string_id", `{"jsonrpc":"2.0","id":"abc","method":"m","params":{}}`, kindRequest, 0},
		{"notification", `{"jsonrpc":"2.0","method":"m"}`, kindNotification, 0},
		{"response_result", `{"jsonrpc":"2.0","id":1,"result":{"a":1}}`, kindResponse, 0},
		{"response_null_result", `{"jsonrpc":"2.0","id":1,"result":null}`, kindResponse, 0},
		{"response_error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"x"}}`, kindResponse, 0},
		{"invalid_json", `{"jsonrpc":`, 0, protocol.CodeParseError},
		{"array_batch", `[{"jsonrpc":"2.0","method":"m"}]`, 0, protocol.CodeInvalidRequest},
		{"wrong_version", `{"jsonrpc":"1.0","id":7,"method":"m"}`, 0, protocol.CodeInvalidRequest},
		{"null_id_request", `{"jsonrpc":"2.0","id":null,"method":"m"}`, 0, protocol.CodeInvalidRequest},
		{"object_id_request", `{"jsonrpc":"2.0","id":{},"method":"m"}`, 0, protocol.CodeInvalidRequest},
		{"method_with_result", `{"jsonrpc":"2.0","id":1,"method":"m","result":1}`, 0, protocol.CodeInvalidRequest},
		{"response_without_id", `{"jsonrpc":"2.0","result":1}`, 0, protocol.CodeInvalidRequest},
		{"response_with_both", `{"jsonrpc":"2.0","id":1,"result":1,"error":{"code":1,"message":"x"}}`, 0, protocol.CodeInvalidRequest},
		{"response_with_neither", `{"jsonrpc":"2.0","id":1}`, 0, protocol.CodeInvalidRequest},
		{"error_member_wrong_type", `{"jsonrpc":"2.0","id":1,"error":"boom"}`, 0, protocol.CodeInvalidRequest},
		{"scalar", `42`, 0, protocol.CodeInvalidRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, k, rerr := decode([]byte(tc.in))
			if tc.code == 0 {
				if rerr != nil || k != tc.kind {
					t.Fatalf("decode = kind %d err %v, want kind %d", k, rerr, tc.kind)
				}
				return
			}
			if rerr == nil || rerr.Code != tc.code || k != 0 {
				t.Fatalf("decode = kind %d err %v, want code %d", k, rerr, tc.code)
			}
		})
	}
}

func TestReplyID(t *testing.T) {
	m, _, rerr := decode([]byte(`{"jsonrpc":"1.0","id":7,"method":"m"}`))
	if rerr == nil {
		t.Fatal("want an error")
	}
	if got := string(replyID(m)); got != "7" {
		t.Fatalf("replyID = %s, want 7", got)
	}
	m, _, _ = decode([]byte(`{"jsonrpc":`))
	if got := string(replyID(m)); got != "null" {
		t.Fatalf("replyID for a parse error = %s, want null", got)
	}
}
