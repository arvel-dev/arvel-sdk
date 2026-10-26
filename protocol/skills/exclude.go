// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/arvel-dev/arvel-sdk/procx"
)

const (
	gitTimeout  = 10 * time.Second
	lockTimeout = 5 * time.Second
	lockStale   = 30 * time.Second
	lockPoll    = 20 * time.Millisecond
)

// gitInfo asks git for the prefix of cwd inside its work tree and the path of
// the info/exclude file that applies to it (the common dir for linked
// worktrees). ok is false when git is missing or cwd is not in a work tree.
func gitInfo(cwd string) (prefix, excludePath string, ok bool) {
	gitPath, err := procx.FindBinary("git")
	if err != nil {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	var out bytes.Buffer
	p, err := procx.Start(ctx, procx.Spec{
		Path:   gitPath,
		Args:   []string{"rev-parse", "--show-prefix", "--git-path", "info/exclude"},
		Dir:    cwd,
		Stdout: &out,
	})
	if err != nil {
		return "", "", false
	}
	if code, werr := p.Wait(); werr != nil || code != 0 {
		return "", "", false
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimRight(out.String(), "\r\n"), "\r", ""), "\n")
	if len(lines) != 2 || lines[1] == "" {
		return "", "", false
	}
	excludePath = lines[1]
	if !filepath.IsAbs(excludePath) {
		excludePath = filepath.Join(cwd, excludePath)
	}
	return lines[0], excludePath, true
}

// lockFile creates path as an exclusive lock file, waiting up to 5 s. A lock
// older than 30 s is treated as stale and removed.
func lockFile(path string) (unlock func(), err error) {
	deadline := time.Now().Add(lockTimeout)
	for {
		f, oerr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if oerr == nil {
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(oerr, fs.ErrExist) {
			return nil, fmt.Errorf("skills: lock %s: %w", path, oerr)
		}
		if fi, serr := os.Stat(path); serr == nil && time.Since(fi.ModTime()) > lockStale {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("skills: lock %s: timed out", path)
		}
		time.Sleep(lockPoll)
	}
}

func readLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n"), nil
}

func writeLines(path string, lines []string) error {
	text := strings.Join(lines, "\n")
	if text != "" {
		text += "\n"
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// addLines appends the lines that are not yet in the exclude file and returns
// the ones it added.
func addLines(excludePath string, lines []string) (added []string, err error) {
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return nil, fmt.Errorf("skills: %w", err)
	}
	unlock, err := lockFile(excludePath + ".arvel-lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	cur, err := readLines(excludePath)
	if err != nil {
		return nil, fmt.Errorf("skills: read %s: %w", excludePath, err)
	}
	for _, l := range lines {
		if !slices.Contains(cur, l) {
			cur = append(cur, l)
			added = append(added, l)
		}
	}
	if len(added) == 0 {
		return nil, nil
	}
	if err := writeLines(excludePath, cur); err != nil {
		return nil, fmt.Errorf("skills: write %s: %w", excludePath, err)
	}
	return added, nil
}

// removeLines drops exactly the given lines from the exclude file.
func removeLines(excludePath string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	if _, err := os.Stat(excludePath); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	unlock, err := lockFile(excludePath + ".arvel-lock")
	if err != nil {
		return err
	}
	defer unlock()
	cur, err := readLines(excludePath)
	if err != nil {
		return fmt.Errorf("skills: read %s: %w", excludePath, err)
	}
	kept := cur[:0:0]
	for _, l := range cur {
		if !slices.Contains(lines, l) {
			kept = append(kept, l)
		}
	}
	return writeLines(excludePath, kept)
}
