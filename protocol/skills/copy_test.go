// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func makeSkill(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	write(t, filepath.Join(dir, "SKILL.md"), "---\nname: "+name+"\n---\nBody of "+name+"\n", 0o644)
	write(t, filepath.Join(dir, "scripts", "run.sh"), "#!/bin/sh\n", 0o755)
	return dir
}

func TestCopyTree_CopiesFilesAndModes(t *testing.T) {
	src := makeSkill(t, t.TempDir(), "pest")
	dst := filepath.Join(t.TempDir(), "out")
	if err := copyTree(src, dst, nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil || string(b) != "---\nname: pest\n---\nBody of pest\n" {
		t.Fatalf("SKILL.md = %q, %v", b, err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dst, "scripts", "run.sh"))
		if err != nil || fi.Mode()&0o111 == 0 {
			t.Fatalf("script lost its executable bit: %v %v", fi, err)
		}
	}
}

func TestCopyTree_AppliesRewriteToSkillMdOnly(t *testing.T) {
	src := makeSkill(t, t.TempDir(), "pest")
	write(t, filepath.Join(src, "sub", "SKILL.md"), "nested\n", 0o644)
	dst := filepath.Join(t.TempDir(), "out")
	err := copyTree(src, dst, func(rel string, data []byte) []byte { return []byte("rewritten") })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "SKILL.md")); string(b) != "rewritten" {
		t.Fatalf("root SKILL.md = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "sub", "SKILL.md")); string(b) != "nested\n" {
		t.Fatalf("nested SKILL.md must be copied verbatim, got %q", b)
	}
}

func TestCopyTree_SkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	src := makeSkill(t, root, "pest")
	secret := filepath.Join(root, "secret.txt")
	write(t, secret, "top secret", 0o644)
	if err := os.Symlink(secret, filepath.Join(src, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := copyTree(src, dst, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "link.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("symlink was copied: %v", err)
	}
}

func TestCopyTree_RequiresSkillMd(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "README.md"), "x", 0o644)
	dst := filepath.Join(t.TempDir(), "out")
	if err := copyTree(src, dst, nil); !errors.Is(err, ErrNotASkill) {
		t.Fatalf("err = %v, want ErrNotASkill", err)
	}
	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("dst must not be created: %v", err)
	}
}

func TestCopyTree_RejectsTooMany(t *testing.T) {
	src := makeSkill(t, t.TempDir(), "big")
	for i := range maxEntries {
		write(t, filepath.Join(src, "files", fmt.Sprintf("f%04d", i)), "", 0o644)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := copyTree(src, dst, nil); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("partial copy must be removed: %v", err)
	}
}

func TestCopyTree_DoesNotOverwrite(t *testing.T) {
	src := makeSkill(t, t.TempDir(), "pest")
	dst := t.TempDir()
	write(t, filepath.Join(dst, "SKILL.md"), "mine", 0o644)
	err := copyTree(src, dst, nil)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v, want fs.ErrExist", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "SKILL.md")); string(b) != "mine" {
		t.Fatalf("existing file was overwritten: %q", b)
	}
}

func TestCopyTree_WritesMarker(t *testing.T) {
	src := makeSkill(t, t.TempDir(), "pest")
	dst := filepath.Join(t.TempDir(), "out")
	if err := copyTree(src, dst, nil); err != nil {
		t.Fatal(err)
	}
	if !hasMarker(dst) {
		t.Fatal("marker missing")
	}
}
