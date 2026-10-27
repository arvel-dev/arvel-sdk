// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckProxyEnv(t *testing.T) {
	const proxy = "http://127.0.0.1:41234"
	correct := map[string]string{
		"HTTPS_PROXY":   proxy,
		"HTTP_PROXY":    proxy,
		"ALL_PROXY":     proxy,
		"NO_PROXY":      "localhost,127.0.0.1",
		"SSH_AUTH_SOCK": "",
	}
	cases := []struct {
		name     string
		reported map[string]string
		want     int
		substr   string
	}{
		{"correct", correct, 0, ""},
		{"mismatched https", map[string]string{
			"HTTPS_PROXY": "http://wrong", "HTTP_PROXY": proxy, "ALL_PROXY": proxy, "NO_PROXY": "localhost,127.0.0.1",
		}, 1, "HTTPS_PROXY"},
		{"ssh auth sock present", map[string]string{
			"HTTPS_PROXY": proxy, "HTTP_PROXY": proxy, "ALL_PROXY": proxy, "NO_PROXY": "localhost,127.0.0.1", "SSH_AUTH_SOCK": "/tmp/ssh-agent",
		}, 1, "SSH_AUTH_SOCK"},
		{"no_proxy missing", map[string]string{
			"HTTPS_PROXY": proxy, "HTTP_PROXY": proxy, "ALL_PROXY": proxy,
		}, 1, "NO_PROXY"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkProxyEnv(c.reported, proxy)
			if len(got) != c.want {
				t.Fatalf("violations = %v, want %d", got, c.want)
			}
			if c.substr != "" && !strings.Contains(strings.Join(got, "; "), c.substr) {
				t.Fatalf("violations %v do not mention %q", got, c.substr)
			}
		})
	}
}

func TestRunProxyCheck_EchoSkips(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the example binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/echo", "echo")
	f := &flags{binary: bin}
	res := runProxyCheck(context.Background(), f, filepath.Join("..", "..", "examples", "echo", "conformance"))
	if res.Status != StatusSkip {
		t.Fatalf("status = %s, want skip (detail=%s)", res.Status, res.Detail)
	}
}

func TestRunProxyCheck_StubPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub and fakevendor binaries")
	}
	t.Setenv("STUB_MODE", "proxy")
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/stubprovider", "stub")
	f := &flags{binary: bin}
	res := runProxyCheck(context.Background(), f, filepath.Join("testdata", "release", "conformance"))
	if res.Status != StatusPass {
		t.Fatalf("status = %s, want pass (detail=%s)", res.Status, res.Detail)
	}
}
