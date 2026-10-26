// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// FindBinary looks for the program name in the PATH directories and then in
// the extra directories, and returns its absolute, symlink-resolved path.
//
// An extra directory may start with "~" (the home directory) and may contain
// %NAME% references to environment variables. Relative directories, including
// relative PATH entries such as ".", are ignored, so the current directory is
// never searched. On Windows the candidates are name.exe, name.cmd, name.bat
// and name.com, restricted to the extensions listed in PATHEXT (other PATHEXT
// entries such as .vbs cannot be started and are ignored).
//
// The error wraps ErrNotFound when nothing matches.
func FindBinary(name string, extra ...string) (string, error) {
	dirs := searchDirs(runtime.GOOS, os.Getenv("PATH"), extra, os.Getenv, homeDir())
	return findIn(runtime.GOOS, name, dirs, os.Getenv("PATHEXT"))
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

func validName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("procx: binary name %q must be a bare name without a path separator", name)
	}
	return nil
}

// searchDirs returns the absolute directories to search: the PATH entries, then
// extra, expanded, de-duplicated and in order.
func searchDirs(goos, pathEnv string, extra []string, getenv func(string) string, home string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(d string, expand bool) {
		if goos == "windows" && len(d) >= 2 && d[0] == '"' && d[len(d)-1] == '"' {
			d = d[1 : len(d)-1]
		}
		if expand {
			d = expandDir(d, getenv, home)
		}
		if d == "" || !filepath.IsAbs(d) || seen[d] {
			return
		}
		seen[d] = true
		out = append(out, d)
	}
	// PATH entries are already expanded by the OS; a literal '%' in one is part
	// of the directory name. Only the caller's extra directories are expanded.
	for _, d := range filepath.SplitList(pathEnv) {
		add(d, false)
	}
	for _, d := range extra {
		add(d, true)
	}
	return out
}

// expandDir expands a leading "~" and every %NAME% reference. It returns ""
// when the directory cannot be expanded.
func expandDir(d string, getenv func(string) string, home string) string {
	if d == "~" || strings.HasPrefix(d, "~/") || strings.HasPrefix(d, `~\`) {
		if home == "" {
			return ""
		}
		d = home + d[1:]
	}
	for {
		i := strings.IndexByte(d, '%')
		if i < 0 {
			return d
		}
		j := strings.IndexByte(d[i+1:], '%')
		if j < 0 {
			return d
		}
		val := getenv(d[i+1 : i+1+j])
		if val == "" {
			return ""
		}
		d = d[:i] + val + d[i+j+2:]
	}
}

// candidateNames lists the file names to try for name in one directory.
func candidateNames(goos, name, pathext string) []string {
	if goos != "windows" {
		return []string{name}
	}
	startable := []string{".exe", ".cmd", ".bat", ".com"}
	listed := map[string]bool{}
	if strings.TrimSpace(pathext) == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	for _, e := range strings.Split(pathext, ";") {
		listed[strings.ToLower(strings.TrimSpace(e))] = true
	}
	ext := strings.ToLower(filepath.Ext(name))
	for _, s := range startable {
		if ext == s {
			return []string{name}
		}
	}
	var out []string
	for _, s := range startable {
		if listed[s] {
			out = append(out, name+s)
		}
	}
	return out
}

func isExecutable(goos string, fi os.FileInfo) bool {
	if !fi.Mode().IsRegular() {
		return false
	}
	return goos == "windows" || fi.Mode()&0o111 != 0
}

func findIn(goos, name string, dirs []string, pathext string) (string, error) {
	if err := validName(name); err != nil {
		return "", err
	}
	names := candidateNames(goos, name, pathext)
	for _, dir := range dirs {
		for _, c := range names {
			p := filepath.Join(dir, c)
			fi, err := os.Stat(p)
			if err != nil || !isExecutable(goos, fi) {
				continue
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				continue
			}
			resolved, err := filepath.EvalSymlinks(abs)
			if err != nil {
				continue
			}
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%w: %q (searched %d directories)", ErrNotFound, name, len(dirs))
}
