// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// result is what Materialize has done so far; undo reverses it.
type result struct {
	folders []string // created skill folders
	lines   []string // exclude lines this call added
	exclude string   // path of the exclude file
	parents []string // directories created on the way to the skills dir
}

func (r *result) undo() {
	for _, f := range r.folders {
		if hasMarker(f) { // never delete a folder we did not create
			_ = os.RemoveAll(f)
		}
	}
	if r.exclude != "" {
		_ = removeLines(r.exclude, r.lines)
	}
	for i := len(r.parents) - 1; i >= 0; i-- {
		_ = os.Remove(r.parents[i]) // only succeeds when empty
	}
}

// Materialize copies the skill folders of refs into the first directory of dirs
// (relative to cwd), excludes the copies from git, and returns a function that
// removes them again. The returned function is never nil and is safe to call
// more than once; on error everything done so far has been rolled back.
func Materialize(cwd string, dirs []string, refs []protocol.SkillRef) (cleanup func(), err error) {
	noop := func() {}
	if len(refs) == 0 {
		return noop, nil
	}
	if len(dirs) == 0 {
		return noop, fmt.Errorf("the vendor announced no project skills directory: %w", ErrBadDir)
	}
	dir := filepath.Clean(dirs[0])
	if dir == "." || !filepath.IsLocal(dir) {
		return noop, fmt.Errorf("%q: %w", dirs[0], ErrBadDir)
	}
	if err := validateRefs(refs); err != nil {
		return noop, err
	}

	res := &result{}
	defer func() {
		if err != nil {
			res.undo()
			cleanup = noop
		}
	}()

	target := filepath.Join(cwd, dir)
	prefix, exclude, haveGit := gitInfo(cwd)

	// The lock makes "check which names are free, then create them" atomic for
	// concurrent sessions of one worktree. A concurrent cleanup may remove the
	// empty skills directory between mkdirAll and the lock, so retry briefly.
	var unlock func()
	for attempt := 0; ; attempt++ {
		if err := mkdirAll(cwd, dir, res); err != nil {
			return noop, err
		}
		if err := confined(cwd, target); err != nil {
			return noop, err
		}
		unlock, err = lockFile(filepath.Join(target, ".arvel-lock"))
		if err == nil {
			break
		}
		if attempt >= 3 || !errors.Is(err, os.ErrNotExist) {
			return noop, err
		}
	}
	defer unlock()

	var lines []string
	for _, ref := range refs {
		final, err := chooseName(target, ref.Name)
		if err != nil {
			return noop, err
		}
		renamed := ""
		if final != ref.Name {
			renamed = final
		}
		manual := ref.Invocation == protocol.SkillInvocationManual
		dst := filepath.Join(target, final)
		err = copyTree(ref.Path, dst, func(_ string, data []byte) []byte {
			return rewriteFrontMatter(data, renamed, manual)
		})
		if err != nil {
			return noop, err
		}
		res.folders = append(res.folders, dst)
		lines = append(lines, "/"+filepath.ToSlash(prefix)+filepath.ToSlash(dir)+"/"+final+"/")
	}
	if haveGit {
		added, err := addLines(exclude, lines)
		if err != nil {
			return noop, err
		}
		res.exclude, res.lines = exclude, added
	}

	var once sync.Once
	return func() { once.Do(res.undo) }, nil
}

// confined rejects a skills directory that resolves, through symbolic links in
// the repository, to a place outside the working directory.
func confined(cwd, target string) error {
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return fmt.Errorf("skills: %w", err)
	}
	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("skills: %w", err)
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%q resolves outside the working directory: %w", target, ErrBadDir)
	}
	return nil
}

func validateRefs(refs []protocol.SkillRef) error {
	seen := map[string]bool{}
	for _, r := range refs {
		if !validName(r.Name) {
			return fmt.Errorf("%q: %w", r.Name, ErrBadName)
		}
		if !r.Invocation.Valid() {
			return fmt.Errorf("skill %q has invocation %q: %w", r.Name, r.Invocation, ErrBadName)
		}
		key := strings.ToLower(r.Name)
		if seen[key] {
			return fmt.Errorf("%q listed twice: %w", r.Name, ErrNameTaken)
		}
		seen[key] = true
	}
	return nil
}

// mkdirAll creates cwd/dir and remembers which directories it created.
func mkdirAll(cwd, dir string, res *result) error {
	path := cwd
	for _, part := range strings.Split(dir, string(filepath.Separator)) {
		path = filepath.Join(path, part)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("skills: %w", err)
		}
		err := os.Mkdir(path, 0o755)
		if errors.Is(err, os.ErrExist) {
			continue // a concurrent call created it; it is not ours to remove
		}
		if err != nil {
			return fmt.Errorf("skills: %w", err)
		}
		res.parents = append(res.parents, path)
	}
	return nil
}

// chooseName picks the folder name for a skill: its own name when free (a stale
// copy of ours is replaced), otherwise the "arvel-" form.
func chooseName(target, name string) (string, error) {
	for _, candidate := range []string{name, "arvel-" + name} {
		p := filepath.Join(target, candidate)
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("skills: %w", err)
		}
		if hasMarker(p) { // stale copy of a crashed session
			if err := os.RemoveAll(p); err != nil {
				return "", fmt.Errorf("skills: %w", err)
			}
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%q: %w", name, ErrNameTaken)
}
