// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/arvel-dev/arvel-sdk/internal/jsoneq"
	"github.com/arvel-dev/arvel-sdk/schema"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSection32RoundTrip(t *testing.T) {
	for _, name := range []string{"section-3.2.json", "section-3.2-command-entry.json", "section-4-connections.json", "valid-claude.json"} {
		raw := readTestdata(t, name)
		m, err := Load(bytesReader(raw))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		back, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		// The fragment files hold only a part of a manifest: compare that part.
		var want, got map[string]any
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(back, &got); err != nil {
			t.Fatal(err)
		}
		sub := map[string]any{}
		for k := range want {
			sub[k] = got[k]
		}
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(sub)
		if ok, diff := jsoneq.Equal(a, b); !ok {
			t.Errorf("%s: %s", name, diff)
		}
	}
}

func TestEntryForAllSixPlatforms(t *testing.T) {
	m := fixture(t, "valid-claude.json", false)
	for _, p := range [][2]string{{"darwin", "arm64"}, {"darwin", "amd64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}} {
		ent, ok := m.EntryFor(p[0], p[1])
		if !ok || ent.Asset == "" || ent.Bin == "" {
			t.Errorf("%s/%s: %+v %v", p[0], p[1], ent, ok)
		}
	}
	if ent, _ := m.EntryFor("windows", "amd64"); ent.Bin != "arvel-plugin-claude.exe" {
		t.Errorf("windows bin = %q", ent.Bin)
	}
}

func TestSchemaURLMatchesExample(t *testing.T) {
	m := fixture(t, "valid-claude.json", false)
	if m.Schema != schema.ID {
		t.Fatalf("$schema = %q, want %q", m.Schema, schema.ID)
	}
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
