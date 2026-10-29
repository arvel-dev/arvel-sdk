// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/testkit/mockhost"
)

func strictMeta(t *testing.T, bin string) protocol.InitMeta {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return protocol.InitMeta{DataDir: dir, Connection: protocol.ConnectionInfo{Kind: "synthetic", Config: map[string]any{"executable": bin, "enabled": false, "count": 2}, Secrets: map[string]string{"token": "synthetic-test-token"}}}
}

func TestStrictFixture(t *testing.T) {
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/strictprovider", "strict")
	for _, tc := range []struct {
		name   string
		change func(*protocol.InitMeta)
		valid  bool
	}{
		{"absolute", func(*protocol.InitMeta) {}, true},
		{"unknown kind", func(m *protocol.InitMeta) { m.Connection.Kind = "unknown" }, false},
		{"missing dataDir", func(m *protocol.InitMeta) { m.DataDir = "" }, false},
		{"relative dataDir", func(m *protocol.InitMeta) { m.DataDir = filepath.Join("relative", "data") }, false},
		{"missing config", func(m *protocol.InitMeta) { m.Connection.Config = nil }, false},
		{"missing secret", func(m *protocol.InitMeta) { m.Connection.Secrets = nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := mockhost.StartDriver(bin)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			meta := strictMeta(t, bin)
			tc.change(&meta)
			meta.PluginAPI = protocol.PluginAPI
			err = d.Call(context.Background(), protocol.MethodInitialize, map[string]any{"protocolVersion": 1, "_meta": protocol.WithMeta(nil, meta)}, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("initialization validity = %v, want %v", err == nil, tc.valid)
			}
		})
	}
}

func TestEnvironmentRegression(t *testing.T) {
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/strictprovider", "strict")
	t.Run("missing metadata gets private data directory", func(t *testing.T) {
		d, err := mockhost.StartDriver(bin)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		meta := strictMeta(t, bin)
		meta.DataDir = ""
		if _, err = d.Initialize(meta); err != nil {
			t.Fatalf("initialize: %v", err)
		}
	})
	t.Run("session uses private workspace", func(t *testing.T) {
		d, err := mockhost.StartDriver(bin)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		if _, err = d.Initialize(strictMeta(t, bin)); err != nil {
			t.Fatal(err)
		}
		id, err := d.NewSession(protocol.SessionMeta{})
		if err != nil {
			t.Fatalf("session/new: %v", err)
		}
		if err = d.LoadSession(id, protocol.SessionMeta{}); err != nil {
			t.Fatalf("session/load: %v", err)
		}
	})
}
