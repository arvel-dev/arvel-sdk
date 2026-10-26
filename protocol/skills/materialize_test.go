// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func initRepo(t *testing.T) string {
	t.Helper()
	needGit(t)
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "t")
	git(t, dir, "config", "commit.gpgsign", "false")
	write(t, filepath.Join(dir, "README.md"), "x\n", 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "init")
	return dir
}

func refs(t *testing.T, names ...string) []protocol.SkillRef {
	t.Helper()
	src := t.TempDir()
	var out []protocol.SkillRef
	for _, n := range names {
		inv := protocol.SkillInvocationAuto
		if strings.HasPrefix(n, "manual-") {
			inv = protocol.SkillInvocationManual
		}
		out = append(out, protocol.SkillRef{Name: n, Path: makeSkill(t, src, n), Invocation: inv})
	}
	return out
}

var skillDirs = []string{".vendor/skills"}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

func excludeFile(t *testing.T, dir string) string {
	t.Helper()
	p := git(t, dir, "rev-parse", "--git-path", "info/exclude")
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return p
}

func TestMaterialize_CopiesAndExcludes(t *testing.T) {
	repo := initRepo(t)
	rs := refs(t, "pest-tests", "manual-notes")
	cleanup, err := Materialize(repo, skillDirs, rs)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"pest-tests", "manual-notes"} {
		if !exists(filepath.Join(repo, ".vendor/skills", n, "SKILL.md")) {
			t.Fatalf("%s not copied", n)
		}
	}
	if out := git(t, repo, "status", "--porcelain"); out != "" {
		t.Fatalf("copies must be invisible to git, status: %q", out)
	}
	b, _ := os.ReadFile(filepath.Join(repo, ".vendor/skills/manual-notes/SKILL.md"))
	if !strings.Contains(string(b), "disable-model-invocation: true") {
		t.Fatalf("manual skill lacks the switch: %q", b)
	}
	b, _ = os.ReadFile(filepath.Join(repo, ".vendor/skills/pest-tests/SKILL.md"))
	if strings.Contains(string(b), "disable-model-invocation") {
		t.Fatalf("auto skill must not get the switch: %q", b)
	}
	src, _ := os.ReadFile(filepath.Join(rs[1].Path, "SKILL.md"))
	if strings.Contains(string(src), "disable-model-invocation") {
		t.Fatal("the source folder was modified")
	}
	cleanup()
}

func TestMaterialize_PrefixesOnCollision(t *testing.T) {
	repo := initRepo(t)
	own := filepath.Join(repo, ".vendor/skills/pest-tests")
	write(t, filepath.Join(own, "SKILL.md"), "---\nname: pest-tests\n---\nrepo's own\n", 0o644)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "own skill")

	cleanup, err := Materialize(repo, skillDirs, refs(t, "pest-tests"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(repo, ".vendor/skills/arvel-pest-tests/SKILL.md"))
	if err != nil || !strings.Contains(string(b), "name: arvel-pest-tests") {
		t.Fatalf("prefixed copy = %q, %v", b, err)
	}
	if b, _ := os.ReadFile(filepath.Join(own, "SKILL.md")); !strings.Contains(string(b), "repo's own") {
		t.Fatal("the repository's skill was changed")
	}
	if out := git(t, repo, "status", "--porcelain"); out != "" {
		t.Fatalf("status: %q", out)
	}
	cleanup()
	if !exists(filepath.Join(own, "SKILL.md")) {
		t.Fatal("cleanup removed the repository's own skill")
	}
	if exists(filepath.Join(repo, ".vendor/skills/arvel-pest-tests")) {
		t.Fatal("cleanup left the prefixed copy")
	}
}

func TestMaterialize_Worktree(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	git(t, repo, "worktree", "add", "-b", "side", wt)
	cleanup, err := Materialize(wt, skillDirs, refs(t, "pest-tests"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	b, _ := os.ReadFile(excludeFile(t, wt))
	if !strings.Contains(string(b), "/.vendor/skills/pest-tests/") {
		t.Fatalf("common info/exclude lacks the line:\n%s", b)
	}
	if out := git(t, wt, "status", "--porcelain"); out != "" {
		t.Fatalf("status in the worktree: %q", out)
	}
}

func TestMaterialize_SubdirectoryCwd(t *testing.T) {
	repo := initRepo(t)
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cleanup, err := Materialize(sub, skillDirs, refs(t, "pest-tests"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	b, _ := os.ReadFile(excludeFile(t, repo))
	if !strings.Contains(string(b), "/sub/.vendor/skills/pest-tests/") {
		t.Fatalf("exclude lacks the prefixed line:\n%s", b)
	}
	if out := git(t, repo, "status", "--porcelain"); out != "" {
		t.Fatalf("status: %q", out)
	}
}

func TestMaterialize_CleanupRemovesEverything(t *testing.T) {
	repo := initRepo(t)
	before, _ := os.ReadFile(excludeFile(t, repo))
	cleanup, err := Materialize(repo, skillDirs, refs(t, "a-skill", "b-skill"))
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	cleanup() // idempotent
	if exists(filepath.Join(repo, ".vendor")) {
		t.Fatal("created parent directories were left behind")
	}
	after, _ := os.ReadFile(excludeFile(t, repo))
	if string(before) != string(after) {
		t.Fatalf("info/exclude changed:\nbefore %q\nafter  %q", before, after)
	}
	if out := git(t, repo, "status", "--porcelain", "--ignored"); out != "" {
		t.Fatalf("leftovers: %q", out)
	}
}

func TestMaterialize_StaleCopyIsReplaced(t *testing.T) {
	repo := initRepo(t)
	rs := refs(t, "pest-tests")
	stale := filepath.Join(repo, ".vendor/skills/pest-tests")
	write(t, filepath.Join(stale, "SKILL.md"), "old", 0o644)
	write(t, filepath.Join(stale, markerFile), "x", 0o644)
	cleanup, err := Materialize(repo, skillDirs, rs)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if b, _ := os.ReadFile(filepath.Join(stale, "SKILL.md")); strings.Contains(string(b), "old") {
		t.Fatalf("stale copy kept: %q", b)
	}
}

func TestMaterialize_NameTakenRollsBack(t *testing.T) {
	repo := initRepo(t)
	skills := filepath.Join(repo, ".vendor/skills")
	write(t, filepath.Join(skills, "second", "SKILL.md"), "own", 0o644)         // repo skill
	write(t, filepath.Join(skills, "arvel-second", "SKILL.md"), "other", 0o644) // not ours either
	_, err := Materialize(repo, skillDirs, refs(t, "first", "second"))
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("err = %v, want ErrNameTaken", err)
	}
	if exists(filepath.Join(skills, "first")) {
		t.Fatal("the first copy was not rolled back")
	}
}

func TestMaterialize_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	cleanup, err := Materialize(dir, skillDirs, refs(t, "pest-tests"))
	if err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dir, ".vendor/skills/pest-tests/SKILL.md")) {
		t.Fatal("not copied")
	}
	cleanup()
	if exists(filepath.Join(dir, ".vendor")) {
		t.Fatal("cleanup left directories")
	}
}

func TestMaterialize_NoRefsIsNoop(t *testing.T) {
	cleanup, err := Materialize(t.TempDir(), nil, nil)
	if err != nil || cleanup == nil {
		t.Fatalf("cleanup nil = %v, err = %v", cleanup == nil, err)
	}
	cleanup()
}

func TestMaterialize_Validation(t *testing.T) {
	good := refs(t, "ok")
	dup := append(refs(t, "same"), refs(t, "SAME")...)
	badInv := refs(t, "x")
	badInv[0].Invocation = "sometimes"
	badName := []protocol.SkillRef{{Name: "../evil", Path: good[0].Path, Invocation: protocol.SkillInvocationAuto}}
	noSkill := []protocol.SkillRef{{Name: "empty", Path: t.TempDir(), Invocation: protocol.SkillInvocationAuto}}
	tests := []struct {
		name string
		dirs []string
		refs []protocol.SkillRef
		want error
	}{
		{"empty_dirs", nil, good, ErrBadDir},
		{"absolute_dir", []string{filepath.Join(os.TempDir(), "x")}, good, ErrBadDir},
		{"parent_dir", []string{"../x"}, good, ErrBadDir},
		{"dot_dir", []string{"."}, good, ErrBadDir},
		{"bad_name", skillDirs, badName, ErrBadName},
		{"duplicate_names_differing_in_case", skillDirs, dup, ErrNameTaken},
		{"invalid_invocation", skillDirs, badInv, ErrBadName},
		{"source_without_skill_md", skillDirs, noSkill, ErrNotASkill},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			cleanup, err := Materialize(cwd, tc.dirs, tc.refs)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if cleanup == nil {
				t.Fatal("cleanup must never be nil")
			}
			cleanup()
			entries, _ := os.ReadDir(cwd)
			if len(entries) != 0 {
				t.Fatalf("disk not left untouched: %v", entries)
			}
		})
	}
}

func TestMaterialize_ConcurrentCalls(t *testing.T) {
	repo := initRepo(t)
	const n = 8
	var names []string
	for i := range n {
		names = append(names, fmt.Sprintf("skill-%d", i))
	}
	all := refs(t, names...)
	var wg sync.WaitGroup
	cleanups := make([]func(), n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := Materialize(repo, skillDirs, all[i:i+1])
			if err != nil {
				t.Error(err)
				return
			}
			cleanups[i] = c
		}()
	}
	wg.Wait()
	b, _ := os.ReadFile(excludeFile(t, repo))
	for _, name := range names {
		if strings.Count(string(b), "/.vendor/skills/"+name+"/") != 1 {
			t.Fatalf("exclude must hold %s exactly once:\n%s", name, b)
		}
	}
	for _, c := range cleanups {
		if c != nil {
			c()
		}
	}
	after, _ := os.ReadFile(excludeFile(t, repo))
	if strings.Contains(string(after), ".vendor/skills") {
		t.Fatalf("lines left after cleanup:\n%s", after)
	}
}

func TestLockFile_StaleLockRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.arvel-lock")
	write(t, path, "", 0o644)
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if exists(path) {
		t.Fatal("lock file left behind")
	}
}

func TestMaterialize_SymlinkedSkillsDirIsRejected(t *testing.T) {
	cwd := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(cwd, ".vendor")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cleanup, err := Materialize(cwd, skillDirs, refs(t, "pest-tests"))
	if !errors.Is(err, ErrBadDir) {
		t.Fatalf("err = %v, want ErrBadDir", err)
	}
	cleanup()
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("files were written outside the working directory: %v", entries)
	}
}
