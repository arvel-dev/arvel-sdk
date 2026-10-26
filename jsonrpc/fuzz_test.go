// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"strings"
	"testing"
)

func FuzzPeerDecode(f *testing.F) {
	seeds := []string{
		`{"jsonrpc":"2.0","id":1,"method":"m"}`,
		`{"jsonrpc":"2.0","id":"abc","method":"m","params":{"sessionId":"s"}}`,
		`{"jsonrpc":"2.0","method":"m"}`,
		`{"jsonrpc":"2.0","id":1,"result":{"a":1}}`,
		`{"jsonrpc":"2.0","id":1,"result":null}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"x"}}`,
		`[]`,
		`{"jsonrpc":"2.0","method":"x","id":1e999}`,
		strings.Repeat("[", 1<<20),
		"\x00\x00\x00",
		"{\"jsonrpc\":\"2.0\",\"method\":\"\xff\xfe\"}",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, line []byte) {
		m, k, rerr := decode(line)
		if (rerr != nil) == (k != 0) {
			t.Fatalf("exactly one of error and kind must be set: kind %d err %v", k, rerr)
		}
		if k == kindRequest && (len(m.ID) == 0 || m.Method == "") {
			t.Fatalf("request without id or method: %+v", m)
		}
		if k == kindResponse && m.Method != "" {
			t.Fatalf("response with a method: %+v", m)
		}
		_ = replyID(m)
	})
}
