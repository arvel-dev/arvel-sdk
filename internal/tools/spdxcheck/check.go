// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const headerPrefix = "// SPDX-License-Identifier: "

// skipDirs lists directory names that are never scanned.
var skipDirs = map[string]bool{
	".git":         true,
	".ai-factory":  true,
	".claude":      true,
	".codex":       true,
	".agents":      true,
	"testdata":     true,
	"vendor":       true,
	"node_modules": true,
}

// Violation describes a .go file whose first line is not the expected header.
type Violation struct {
	Path string
	Want string
	Got  string
}

// expectedLicense returns the SPDX identifier required for the slash-separated
// path rel, which is relative to the scanned root.
func expectedLicense(rel string) string {
	first, _, _ := strings.Cut(rel, "/")
	if first == "examples" || first == "templates" {
		return "MIT-0"
	}
	return "Apache-2.0"
}

// firstLine returns the first line of the file without its line terminator.
func firstLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Check walks root and reports every .go file with a missing or wrong SPDX
// header. It returns the number of .go files it examined.
func Check(root string, log *slog.Logger) (checked int, violations []Violation, err error) {
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				log.Debug("skip dir", "path", path)
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		want := expectedLicense(rel)

		got, err := firstLine(path)
		if err != nil {
			return fmt.Errorf("spdxcheck: %s: %w", path, err)
		}

		checked++
		ok := got == headerPrefix+want
		log.Debug("checked file", "path", rel, "want", want, "ok", ok)
		if !ok {
			violations = append(violations, Violation{Path: rel, Want: want, Got: got})
		}
		return nil
	})
	if walkErr != nil {
		return checked, violations, walkErr
	}
	return checked, violations, nil
}
