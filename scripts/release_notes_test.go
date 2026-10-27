// SPDX-License-Identifier: Apache-2.0

package scripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReleaseNotes_SelectsExactNonemptySection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX release script runs on Linux and macOS")
	}
	cases := []struct {
		name, changelog string
		args            []string
		want            string
		fail            bool
	}{
		{"exact stable", "## [Unreleased]\nnew\n## [0.1.0-beta.1]\nbeta\n## [0.1.0] - 2026-10-28\n\n### Added\n- First release.\n\n## [0.0.1]\nold\n", []string{"0.1.0"}, "\n### Added\n- First release.\n\n", false},
		{"beta final section", "## [0.1.0-beta.1]\n- Preview.\n", []string{"0.1.0-beta.1"}, "- Preview.\n", false},
		{"missing", "## [0.1.0-beta.1]\n- Preview.\n", []string{"0.1.0"}, "", true},
		{"empty", "## [0.1.0]\n \t\n## [0.0.1]\n- Old.\n", []string{"0.1.0"}, "", true},
		{"heading only", "## [0.1.0]\n### Added\n\n", []string{"0.1.0"}, "", true},
		{"missing file", "", []string{"0.1.0"}, "", true},
		{"no argument", "", nil, "", true},
		{"extra argument", "", []string{"0.1.0", "extra"}, "", true},
		{"tag prefix", "## [v0.1.0]\n- Bad.\n", []string{"v0.1.0"}, "", true},
		{"unsafe version", "", []string{"0.1.0\nother"}, "", true},
	}
	for _, script := range []string{"release-notes.sh", "../templates/scripts/release-notes.sh"} {
		path, err := filepath.Abs(script)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range cases {
			t.Run(script+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				if tc.changelog != "" {
					if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(tc.changelog), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command("sh", append([]string{path}, tc.args...)...)
				cmd.Dir = dir
				var out, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &stderr
				err := cmd.Run()
				if (err != nil) != tc.fail || out.String() != tc.want {
					t.Fatalf("err=%v stdout=%q stderr=%q; want failure=%t stdout=%q", err, out.String(), stderr.String(), tc.fail, tc.want)
				}
				if tc.fail && stderr.Len() == 0 {
					t.Fatal("failure has no diagnostic")
				}
			})
		}
	}
}
