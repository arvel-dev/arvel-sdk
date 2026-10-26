// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name string, size int) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateFiles(t *testing.T) {
	team := func() *Manifest { return fixture(t, "valid-team.json", true) }
	pkg := func(t *testing.T) string {
		d := t.TempDir()
		writeFile(t, d, "roles/docs-writer.en.md", 10)
		writeFile(t, d, "roles/docs-writer.ru.md", 10)
		return d
	}

	t.Run("valid", func(t *testing.T) {
		if ps := team().ValidateFiles(pkg(t)); len(ps) != 0 {
			t.Fatal(ps)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		d := pkg(t)
		if err := os.Remove(filepath.Join(d, "roles", "docs-writer.ru.md")); err != nil {
			t.Fatal(err)
		}
		if _, ok := find(team().ValidateFiles(d), "file-missing", "/contributes/roles/0/instructions/ru"); !ok {
			t.Fatal("missing file not reported")
		}
	})
	t.Run("directory instead of file", func(t *testing.T) {
		d := pkg(t)
		p := filepath.Join(d, "roles", "docs-writer.en.md")
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, ok := find(team().ValidateFiles(d), "file-not-regular", ""); !ok {
			t.Fatal("directory not reported")
		}
	})
	t.Run("size limit", func(t *testing.T) {
		d := pkg(t)
		writeFile(t, d, "roles/docs-writer.en.md", MaxInstructionsSize)
		if ps := team().ValidateFiles(d); len(ps) != 0 {
			t.Fatalf("16 KiB must pass: %v", ps)
		}
		writeFile(t, d, "roles/docs-writer.en.md", MaxInstructionsSize+1)
		if _, ok := find(team().ValidateFiles(d), "instructions-too-large", ""); !ok {
			t.Fatal("16 KiB + 1 must fail")
		}
	})
	t.Run("symlink escaping the package", func(t *testing.T) {
		d := pkg(t)
		outside := t.TempDir()
		writeFile(t, outside, "secret.md", 10)
		link := filepath.Join(d, "roles", "docs-writer.en.md")
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "secret.md"), link); err != nil {
			t.Skipf("symlinks are not available: %v", err)
		}
		if _, ok := find(team().ValidateFiles(d), "path-invalid", ""); !ok {
			t.Fatal("escaping symlink not reported")
		}
	})
	t.Run("no package directory", func(t *testing.T) {
		if _, ok := find(team().ValidateFiles(filepath.Join(t.TempDir(), "nope")), "package-dir", ""); !ok {
			t.Fatal("missing directory not reported")
		}
	})
	t.Run("invalid paths are left to Validate", func(t *testing.T) {
		m := team()
		m.Contributes.Roles[0].Instructions["en"] = "/etc/passwd"
		if ps := m.ValidateFiles(pkg(t)); len(ps) != 0 {
			t.Fatal(ps)
		}
		if _, ok := find(m.Validate(), "path-invalid", ""); !ok {
			t.Fatal("Validate must report the path")
		}
	})
	t.Run("nothing to check", func(t *testing.T) {
		if ps := fixture(t, "valid-claude.json", false).ValidateFiles("/nonexistent"); ps != nil {
			t.Fatal(ps)
		}
	})
}

func TestValidateFiles_Skills(t *testing.T) {
	integ := func() *Manifest { return fixture(t, "valid-integration.json", true) }
	t.Run("valid", func(t *testing.T) {
		d := t.TempDir()
		writeFile(t, d, "skills/godot-scenes/SKILL.md", 5)
		if ps := integ().ValidateFiles(d); len(ps) != 0 {
			t.Fatal(ps)
		}
	})
	t.Run("missing folder", func(t *testing.T) {
		if _, ok := find(integ().ValidateFiles(t.TempDir()), "file-missing", "/contributes/skills/0/path"); !ok {
			t.Fatal("missing folder not reported")
		}
	})
	t.Run("no SKILL.md", func(t *testing.T) {
		d := t.TempDir()
		writeFile(t, d, "skills/godot-scenes/README.md", 5)
		if _, ok := find(integ().ValidateFiles(d), "skill-md-missing", ""); !ok {
			t.Fatal("missing SKILL.md not reported")
		}
	})
	t.Run("file instead of folder", func(t *testing.T) {
		d := t.TempDir()
		writeFile(t, d, "skills/godot-scenes", 5)
		if _, ok := find(integ().ValidateFiles(d), "file-not-regular", ""); !ok {
			t.Fatal("file not reported")
		}
	})
}
