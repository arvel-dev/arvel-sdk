// SPDX-License-Identifier: MIT-0

package teamsample

import (
	"os"
	"strings"
	"testing"

	"github.com/arvel-dev/arvel-sdk/manifest"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

func load(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.LoadFile("arvel.json")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestManifestIsValid(t *testing.T) {
	m := load(t)
	if ps := m.ValidateWith(manifest.Options{Official: true}); len(ps) != 0 {
		t.Fatal(ps)
	}
	if ps := m.ValidateFiles("."); len(ps) != 0 {
		t.Fatal(ps)
	}
	if m.Type != manifest.TypeTeam {
		t.Fatalf("type = %s", m.Type)
	}
}

func TestShape(t *testing.T) {
	m := load(t)
	c := m.Contributes
	if len(c.Roles) != 1 || len(c.TaskTypes) != 1 || len(c.MCPServers) != 0 || len(c.Skills) != 0 {
		t.Fatalf("contributes = %+v", c)
	}
	r := c.Roles[0]
	if r.ID != "docs-writer" || r.Kind != protocol.RoleKindCode || r.Placement.After != protocol.SlotTester {
		t.Fatalf("role = %+v", r)
	}
	ty := c.TaskTypes[0]
	want := map[string]protocol.SlotState{
		protocol.SlotLeadIntake: protocol.SlotStateOn, protocol.PluginSlot(m.ID, "docs-writer"): protocol.SlotStateOn,
		protocol.SlotReviewer: protocol.SlotStateOn, protocol.SlotLeadAccept: protocol.SlotStateOn,
	}
	if ty.ID != "docs" || ty.Branch != manifest.BranchNew || ty.Result != manifest.ResultCode || len(ty.Roles) != len(want) {
		t.Fatalf("type = %+v", ty)
	}
	for slot, st := range want {
		if ty.Roles[slot] != st {
			t.Errorf("roles[%s] = %q, want %q", slot, ty.Roles[slot], st)
		}
	}
}

func TestInstructionsAreSmallAndMentionDocs(t *testing.T) {
	for _, f := range []string{"roles/docs-writer.en.md", "roles/docs-writer.ru.md"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > manifest.MaxInstructionsSize || !strings.Contains(string(b), "docs/") {
			t.Errorf("%s: %d bytes, mentions docs/: %v", f, len(b), strings.Contains(string(b), "docs/"))
		}
	}
}

func TestReleaseWorkflowPacksOnlyData(t *testing.T) {
	b, err := os.ReadFile("release.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{"arvel.json", "roles", "checksums.txt", "--draft"} {
		if !strings.Contains(text, want) {
			t.Errorf("release.yml does not mention %q", want)
		}
	}
	for _, bad := range []string{"go build", "goreleaser"} {
		if strings.Contains(text, bad) {
			t.Errorf("release.yml must not contain %q", bad)
		}
	}
}
