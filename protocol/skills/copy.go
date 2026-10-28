// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Sentinel errors of the package.
var (
	// ErrNotASkill is returned for a source folder without a SKILL.md file.
	ErrNotASkill = errors.New("skills: not a skill folder (SKILL.md missing)")
	// ErrTooLarge is returned for a skill folder over the entry or size limit.
	ErrTooLarge = errors.New("skills: skill folder too large")
	// ErrBadName is returned for an unsafe or duplicate skill name or an invalid invocation.
	ErrBadName = errors.New("skills: invalid skill name")
	// ErrBadDir is returned when the skills directory is missing or not a local relative path.
	ErrBadDir = errors.New("skills: invalid skills directory")
	// ErrNameTaken is returned when neither the skill name nor its "arvel-" form is free.
	ErrNameTaken = errors.New("skills: skill name already taken")
)

const (
	// markerFile records a unique copy owner; its presence alone never proves
	// that a copy is stale or safe to remove.
	markerFile = ".arvel-materialized"
	skillFile  = "SKILL.md"

	maxEntries = 2000
	maxBytes   = 64 << 20
)

// copyTree copies the skill folder src to dst. Symbolic links and other
// non-regular files are skipped. rewrite is applied to the root SKILL.md. On
// failure the partial dst is removed when copyTree created it.
func copyTree(src, dst string, rewrite func(rel string, data []byte) []byte) (err error) {
	if resolved, e := filepath.EvalSymlinks(src); e == nil {
		src = resolved
	}
	if fi, e := os.Stat(filepath.Join(src, skillFile)); e != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%s: %w", src, ErrNotASkill)
	}
	_, statErr := os.Stat(dst)
	created := errors.Is(statErr, fs.ErrNotExist)
	defer func() {
		if err != nil && created {
			_ = os.RemoveAll(dst)
		}
	}()
	if err = os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("skills: create %s: %w", dst, err)
	}

	entries, total := 0, int64(0)
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("skills: path %s escapes the skill folder", path)
		}
		if rel == "." {
			return nil
		}
		if entries++; entries > maxEntries {
			return fmt.Errorf("%s: more than %d entries: %w", src, maxEntries, ErrTooLarge)
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			if total += info.Size(); total > maxBytes {
				return fmt.Errorf("%s: more than %d bytes: %w", src, maxBytes, ErrTooLarge)
			}
			return copyFile(path, target, rel, info.Mode(), rewrite)
		default:
			return nil // symlinks, devices, sockets: skipped
		}
	})
	if err != nil {
		return fmt.Errorf("skills: copy %s: %w", filepath.Base(src), err)
	}
	return writeMarker(dst)
}

func copyFile(src, dst, rel string, mode fs.FileMode, rewrite func(string, []byte) []byte) error {
	perm := fs.FileMode(0o644)
	if mode&0o111 != 0 {
		perm = 0o755
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if rel == skillFile && rewrite != nil {
		data, rerr := io.ReadAll(in)
		if rerr == nil {
			_, rerr = out.Write(rewrite(rel, data))
		}
		return closeAfter(out, rerr)
	}
	_, err = io.Copy(out, in)
	return closeAfter(out, err)
}

func closeAfter(f *os.File, err error) error {
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func writeMarker(dir string) error {
	f, err := os.OpenFile(filepath.Join(dir, markerFile), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("skills: marker in %s: %w", dir, err)
	}
	_, err = io.WriteString(f, "arvel-sdk skills.Materialize\n"+rand.Text()+"\n")
	return closeAfter(f, err)
}

func readMarker(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, markerFile))
	return string(b), err
}
