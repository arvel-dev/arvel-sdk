// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExpectedLicense(t *testing.T) {
	tests := []struct {
		rel  string
		want string
	}{
		{"doc.go", "Apache-2.0"},
		{"protocol/types.go", "Apache-2.0"},
		{"examples/echo/main.go", "MIT-0"},
		{"templates/x/y.go", "MIT-0"},
		{"internal/examples/a.go", "Apache-2.0"},
		{"spikes/s0-1-acp/a.go", "Apache-2.0"},
	}
	for _, tt := range tests {
		if got := expectedLicense(tt.rel); got != tt.want {
			t.Errorf("expectedLicense(%q) = %q, want %q", tt.rel, got, tt.want)
		}
	}
}

func TestCheck(t *testing.T) {
	const apache = "// SPDX-License-Identifier: Apache-2.0\n\npackage x\n"
	const mit = "// SPDX-License-Identifier: MIT-0\n\npackage x\n"

	root := t.TempDir()
	writeFile(t, root, "ok.go", apache)
	writeFile(t, root, "nohdr.go", "package x\n")
	writeFile(t, root, "empty.go", "")
	writeFile(t, root, "examples/echo/main.go", apache)
	writeFile(t, root, "examples/echo/ok.go", mit)
	writeFile(t, root, "protocol/x.go", mit)
	writeFile(t, root, "crlf.go", "// SPDX-License-Identifier: Apache-2.0\r\n\r\npackage x\r\n")
	writeFile(t, root, "build.go", "//go:build linux\n// SPDX-License-Identifier: Apache-2.0\n\npackage x\n")
	writeFile(t, root, "testdata/bad.go", "package x\n")
	writeFile(t, root, ".ai-factory/bad.go", "package x\n")
	writeFile(t, root, "notes.txt", "no header\n")

	checked, violations, err := Check(root, discardLogger())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if checked != 8 {
		t.Errorf("checked = %d, want 8", checked)
	}

	sort.Slice(violations, func(i, j int) bool { return violations[i].Path < violations[j].Path })
	want := []Violation{
		{Path: "build.go", Want: "Apache-2.0", Got: "//go:build linux"},
		{Path: "empty.go", Want: "Apache-2.0", Got: ""},
		{Path: "examples/echo/main.go", Want: "MIT-0", Got: "// SPDX-License-Identifier: Apache-2.0"},
		{Path: "nohdr.go", Want: "Apache-2.0", Got: "package x"},
		{Path: "protocol/x.go", Want: "Apache-2.0", Got: "// SPDX-License-Identifier: MIT-0"},
	}
	if len(violations) != len(want) {
		t.Fatalf("violations = %+v, want %+v", violations, want)
	}
	for i := range want {
		if violations[i] != want[i] {
			t.Errorf("violation %d = %+v, want %+v", i, violations[i], want[i])
		}
	}
}

func TestCheckMissingRoot(t *testing.T) {
	_, _, err := Check(filepath.Join(t.TempDir(), "nope"), discardLogger())
	if err == nil {
		t.Fatal("expected an error for a missing root")
	}
}
