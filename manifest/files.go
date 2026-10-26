// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// ValidateFiles checks the files a team or integration manifest points at, in
// the unpacked package directory dir: every role instruction file exists, is a
// regular file and is at most MaxInstructionsSize bytes, and every skill folder
// holds a SKILL.md (PROTOCOL §16.2, §16.4, §16.5). Paths that Validate already
// rejected are skipped. Files are never read, only inspected; a path that
// escapes dir (also through a symbolic link) is a problem.
func (m *Manifest) ValidateFiles(dir string) []Problem {
	c := m.Contributes
	if c == nil || (len(c.Roles) == 0 && len(c.Skills) == 0) {
		return nil
	}
	var ps problems
	root, err := os.OpenRoot(dir)
	if err != nil {
		ps.err("", "package-dir", "cannot open the package directory: %v", err)
		return ps
	}
	defer root.Close()

	for i, r := range c.Roles {
		for _, lang := range sortedKeys(r.Instructions) {
			name, ok := cleanPackagePath(r.Instructions[lang])
			if !ok {
				continue
			}
			p := fmt.Sprintf("/contributes/roles/%d/instructions/%s", i, lang)
			info, err := root.Stat(name)
			switch {
			case errors.Is(err, fs.ErrNotExist):
				ps.err(p, "file-missing", "%s does not exist in the package", name)
			case err != nil:
				ps.err(p, "path-invalid", "%s cannot be used: %v", name, err)
			case !info.Mode().IsRegular():
				ps.err(p, "file-not-regular", "%s is not a regular file", name)
			case info.Size() > MaxInstructionsSize:
				ps.err(p, "instructions-too-large", "%s is %d bytes; the limit is %d", name, info.Size(), MaxInstructionsSize)
			}
		}
	}
	for i, s := range c.Skills {
		name, ok := cleanPackagePath(s.Path)
		if !ok {
			continue
		}
		p := fmt.Sprintf("/contributes/skills/%d/path", i)
		info, err := root.Stat(name)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			ps.err(p, "file-missing", "%s does not exist in the package", name)
			continue
		case err != nil:
			ps.err(p, "path-invalid", "%s cannot be used: %v", name, err)
			continue
		case !info.IsDir():
			ps.err(p, "file-not-regular", "%s is not a directory", name)
			continue
		}
		md, err := root.Stat(name + "/SKILL.md")
		if err != nil || !md.Mode().IsRegular() {
			ps.err(p, "skill-md-missing", "%s has no SKILL.md", name)
		}
	}
	return ps
}
