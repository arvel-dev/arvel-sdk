// SPDX-License-Identifier: Apache-2.0

package fakevendor

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func run(t *testing.T, yaml string, argv []string, stdin string) (string, string, int) {
	t.Helper()
	s, err := Load(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var out, errBuf bytes.Buffer
	code := Run(context.Background(), s, argv, strings.NewReader(stdin), &out, &errBuf)
	return out.String(), errBuf.String(), code
}

func TestRun_Steps(t *testing.T) {
	yaml := `name: x
steps:
  - emit: { stream: stdout, line: "A" }
  - expect: { json: { type: assistant } }
  - emit: { stream: stderr, line: "B" }
  - sleepMillis: 1
  - exitCode: 7
`
	out, errOut, code := run(t, yaml, nil, `{"type":"assistant"}`)
	if code != 7 {
		t.Fatalf("code = %d", code)
	}
	if strings.TrimSpace(out) != "A" {
		t.Fatalf("stdout = %q", out)
	}
	if strings.TrimSpace(errOut) != "B" {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestRun_VersionAuthStatus(t *testing.T) {
	yaml := `name: x
version: "2.1.310"
authStatus: '{"status":"signed_in"}'
steps:
  - exitCode: 9
`
	out, _, code := run(t, yaml, []string{"--version"}, "")
	if code != 0 || strings.TrimSpace(out) != "2.1.310" {
		t.Fatalf("version: code=%d out=%q", code, out)
	}
	out, _, code = run(t, yaml, []string{"auth", "status"}, "")
	if code != 0 || strings.TrimSpace(out) != `{"status":"signed_in"}` {
		t.Fatalf("auth: code=%d out=%q", code, out)
	}
}

func TestRun_ExpectMismatch(t *testing.T) {
	yaml := `name: x
steps:
  - expect: { json: { type: assistant } }
`
	_, errOut, code := run(t, yaml, nil, `{"type":"system"}`)
	if code != 2 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(errOut, "expectation failed") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestRun_Argv(t *testing.T) {
	yaml := `name: x
steps:
  - expect: { argv: [one, two] }
  - exitCode: 0
`
	if _, _, code := run(t, yaml, []string{"one", "two"}, ""); code != 0 {
		t.Fatalf("match code = %d", code)
	}
	if _, _, code := run(t, yaml, []string{"one"}, ""); code != 2 {
		t.Fatalf("mismatch code = %d", code)
	}
}

func TestRun_Regex(t *testing.T) {
	yaml := `name: x
steps:
  - expect: { regex: '^hello' }
  - exitCode: 0
`
	if _, _, code := run(t, yaml, nil, "hello world"); code != 0 {
		t.Fatalf("match code = %d", code)
	}
	if _, _, code := run(t, yaml, nil, "bye"); code != 2 {
		t.Fatalf("mismatch code = %d", code)
	}
}
