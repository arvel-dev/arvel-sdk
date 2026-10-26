// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func writeExe(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func resolved(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// exeName returns the file name FindBinary can find for a bare "tool" on this OS.
func exeName() string {
	if runtime.GOOS == "windows" {
		return "tool.exe"
	}
	return "tool"
}

func setPath(t *testing.T, dirs ...string) {
	t.Helper()
	t.Setenv("PATH", joinList(dirs))
	t.Setenv("PATHEXT", ".EXE")
}

func joinList(dirs []string) string {
	s := ""
	for i, d := range dirs {
		if i > 0 {
			s += string(os.PathListSeparator)
		}
		s += d
	}
	return s
}

func TestFindBinary_FindsOnPath(t *testing.T) {
	dir := t.TempDir()
	want := writeExe(t, dir, exeName())
	setPath(t, dir)
	got, err := FindBinary("tool")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) || got != resolved(t, want) {
		t.Fatalf("got %q, want %q", got, resolved(t, want))
	}
}

func TestFindBinary_PathBeforeExtra(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	wantA := writeExe(t, a, exeName())
	wantB := writeExe(t, b, exeName())
	setPath(t, a)
	got, err := FindBinary("tool", b)
	if err != nil || got != resolved(t, wantA) {
		t.Fatalf("got %q, %v; want %q", got, err, resolved(t, wantA))
	}
	if err := os.Remove(wantA); err != nil {
		t.Fatal(err)
	}
	got, err = FindBinary("tool", b)
	if err != nil || got != resolved(t, wantB) {
		t.Fatalf("got %q, %v; want %q", got, err, resolved(t, wantB))
	}
}

func TestFindBinary_NotFound(t *testing.T) {
	setPath(t, t.TempDir())
	_, err := FindBinary("definitely-not-here")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestFindIn_SkipsNonExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits are not meaningful on Windows")
	}
	a, b := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(a, "tool"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := writeExe(t, b, "tool")
	got, err := findIn("linux", "tool", []string{a, b}, "")
	if err != nil || got != resolved(t, want) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestFindIn_SkipsDirectories(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(a, "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := writeExe(t, b, "tool")
	got, err := findIn("linux", "tool", []string{a, b}, "")
	if err != nil || got != resolved(t, want) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestFindBinary_ResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := writeExe(t, dir, "target-"+exeName())
	link := filepath.Join(dir, exeName())
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	setPath(t, dir)
	got, err := FindBinary("tool")
	if err != nil || got != resolved(t, target) {
		t.Fatalf("got %q, %v; want %q", got, err, resolved(t, target))
	}
}

func TestSearchDirs(t *testing.T) {
	home := t.TempDir()
	appdata := t.TempDir()
	p1, p2 := t.TempDir(), t.TempDir()
	getenv := func(k string) string {
		if k == "APPDATA" {
			return appdata
		}
		return ""
	}
	list := func(ds ...string) string { return joinList(ds) }

	t.Run("relative and empty entries dropped", func(t *testing.T) {
		got := searchDirs("linux", list(".", "bin", "", p1), nil, getenv, home)
		if !reflect.DeepEqual(got, []string{p1}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("home expansion", func(t *testing.T) {
		got := searchDirs("linux", "", []string{"~/.local/bin"}, getenv, home)
		if !reflect.DeepEqual(got, []string{home + "/.local/bin"}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("home with empty home dropped", func(t *testing.T) {
		if got := searchDirs("linux", "", []string{"~/.local/bin", "~"}, getenv, ""); len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("percent expansion", func(t *testing.T) {
		got := searchDirs("windows", "", []string{"%APPDATA%/npm"}, getenv, home)
		if !reflect.DeepEqual(got, []string{appdata + "/npm"}) {
			t.Fatalf("got %v", got)
		}
		if got := searchDirs("windows", "", []string{"%UNSET%/npm"}, getenv, home); len(got) != 0 {
			t.Fatalf("unset variable kept: %v", got)
		}
	})
	t.Run("windows quotes stripped", func(t *testing.T) {
		got := searchDirs("windows", `"`+p1+`"`, nil, getenv, home)
		if !reflect.DeepEqual(got, []string{p1}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("duplicates collapse and order is PATH then extra", func(t *testing.T) {
		got := searchDirs("linux", list(p1, p2, p1), []string{p2, home}, getenv, home)
		if !reflect.DeepEqual(got, []string{p1, p2, home}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("percent in a PATH entry is literal", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "a%b%c")
		got := searchDirs("linux", dir, nil, getenv, home)
		if !reflect.DeepEqual(got, []string{dir}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("unterminated percent kept as is", func(t *testing.T) {
		if got := expandDir("a%b", getenv, home); got != "a%b" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestValidName(t *testing.T) {
	if err := validName("claude"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`} {
		if validName(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := FindBinary("a/b"); err == nil {
		t.Error("FindBinary accepted a path")
	}
}

func TestCandidateNames(t *testing.T) {
	cases := []struct {
		goos, name, pathext string
		want                []string
	}{
		{"windows", "tool", "", []string{"tool.exe", "tool.cmd", "tool.bat", "tool.com"}},
		{"windows", "tool", ".CMD;.EXE", []string{"tool.exe", "tool.cmd"}},
		{"windows", "tool", ".VBS;.JS", nil},
		{"windows", "tool.cmd", ".EXE;.CMD", []string{"tool.cmd"}},
		{"windows", "TOOL.CMD", ".EXE", []string{"TOOL.CMD"}},
		{"linux", "tool", ".EXE", []string{"tool"}},
	}
	for _, c := range cases {
		if got := candidateNames(c.goos, c.name, c.pathext); !reflect.DeepEqual(got, c.want) {
			t.Errorf("candidateNames(%q, %q, %q) = %v, want %v", c.goos, c.name, c.pathext, got, c.want)
		}
	}
}

func TestFindIn_WindowsPrefersExe(t *testing.T) {
	dir := t.TempDir()
	cmd := writeExe(t, dir, "tool.cmd")
	exe := writeExe(t, dir, "tool.exe")
	got, err := findIn("windows", "tool", []string{dir}, ".CMD;.EXE")
	if err != nil || got != resolved(t, exe) {
		t.Fatalf("got %q, %v; want %q", got, err, exe)
	}
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	got, err = findIn("windows", "tool", []string{dir}, ".CMD;.EXE")
	if err != nil || got != resolved(t, cmd) {
		t.Fatalf("got %q, %v; want %q", got, err, cmd)
	}
}
