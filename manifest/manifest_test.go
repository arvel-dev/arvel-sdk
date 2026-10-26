// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/arvel-dev/arvel-sdk/internal/jsoneq"
)

func TestLoadSection32(t *testing.T) {
	m, err := LoadFile("testdata/section-3.2.json")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "claude" || m.Type != TypeProvider || m.Version != "0.1.0" || m.PluginAPI != "1" {
		t.Fatalf("got %+v", m)
	}
	if len(m.Entry.Platforms) != 6 || m.Entry.IsCommand() {
		t.Fatalf("entry: %+v", m.Entry)
	}
	if m.Sandbox == nil || !m.Sandbox.MacOSKeychain || len(m.Sandbox.VendorPaths.Read) != 2 {
		t.Fatalf("sandbox: %+v", m.Sandbox)
	}
	if len(m.Requires) != 1 || m.Requires[0].Install["docs"] == "" {
		t.Fatalf("requires: %+v", m.Requires)
	}
}

func TestLoadCommandEntry(t *testing.T) {
	frag, err := os.ReadFile("testdata/section-3.2-command-entry.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := Load(strings.NewReader(string(frag)))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Entry.IsCommand() || m.Entry.Command[0] != "gemini" {
		t.Fatalf("entry: %+v", m.Entry)
	}
}

func TestLoadErrors(t *testing.T) {
	big := `{"id":"` + strings.Repeat("a", 1<<20) + `"}`
	cases := map[string]string{
		"invalid json":  `{`,
		"trailing data": `{} {}`,
		"wrong type":    `{"version": 5}`,
		"command mixed": `{"entry":{"command":["x"],"linux-amd64":{"asset":"a","bin":"b"}}}`,
		"entry string":  `{"entry":"x"}`,
		"entry value":   `{"entry":{"linux-amd64":5}}`,
		"too large":     big,
	}
	for name, doc := range cases {
		if _, err := Load(strings.NewReader(doc)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	_, err := Load(strings.NewReader(`{"version": 5}`))
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("type error should name the field: %v", err)
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestLoadReadError(t *testing.T) {
	if _, err := Load(failReader{}); err == nil {
		t.Fatal("want error")
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := LoadFile("testdata/nope.json")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
	if _, err := LoadFile("testdata/not-json.txt"); err == nil {
		t.Fatal("want error")
	}
}

func TestEntryFor(t *testing.T) {
	m, err := LoadFile("testdata/section-3.2.json")
	if err != nil {
		t.Fatal(err)
	}
	ent, ok := m.EntryFor("darwin", "arm64")
	if !ok || ent.Asset != "arvel-plugin-claude_darwin_arm64.tar.gz" || ent.Bin != "arvel-plugin-claude" {
		t.Fatalf("got %+v %v", ent, ok)
	}
	if _, ok := m.EntryFor("freebsd", "amd64"); ok {
		t.Error("freebsd should miss")
	}
	cmd := &Manifest{Entry: EntrySpec{Command: []string{"gemini", "--experimental-acp"}}}
	ent, ok = cmd.EntryFor("plan9", "mips")
	if !ok || ent.Command[0] != "gemini" {
		t.Fatalf("command entry: %+v %v", ent, ok)
	}
	ent.Command[0] = "changed"
	if cmd.Entry.Command[0] != "gemini" {
		t.Error("EntryFor must return a copy")
	}
}

func TestRoundTripEntry(t *testing.T) {
	for _, doc := range []string{
		`{"command":["gemini","--experimental-acp"]}`,
		`{"linux-amd64":{"asset":"a.tar.gz","bin":"p"}}`,
	} {
		var e EntrySpec
		if err := json.Unmarshal([]byte(doc), &e); err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if ok, diff := jsoneq.Equal([]byte(doc), b); !ok {
			t.Errorf("%s: %s", doc, diff)
		}
	}
	if b, _ := json.Marshal(EntrySpec{}); string(b) != "{}" {
		t.Errorf("zero entry: %s", b)
	}
}

func TestPlatformsAndTypes(t *testing.T) {
	if len(Platforms()) != 6 {
		t.Fatal("six platforms expected")
	}
	if !TypeProvider.Valid() || !TypeRouter.Valid() || !TypeTeam.Valid() || !TypeIntegration.Valid() || PluginType("tool").Valid() || PluginType("").Valid() {
		t.Fatal("PluginType.Valid misbehaves")
	}
	if !TypeTeam.Declarative() || !TypeIntegration.Declarative() || TypeProvider.Declarative() || TypeRouter.Declarative() {
		t.Fatal("PluginType.Declarative misbehaves")
	}
}
