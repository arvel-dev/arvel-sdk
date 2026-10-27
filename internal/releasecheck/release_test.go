// SPDX-License-Identifier: Apache-2.0

package releasecheck

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/manifest"
	"gopkg.in/yaml.v3"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Markdown has the same meaning after Git's Windows CRLF checkout conversion.
// Binary archives and checksum inputs deliberately continue using raw read.
func markdown(t *testing.T, path string) []byte {
	t.Helper()
	return bytes.ReplaceAll(read(t, path), []byte("\r\n"), []byte("\n"))
}

func write(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		write(t, filepath.Join(dst, rel), read(t, path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// command owns and waits for every child. Tests never contact vendors or Go
// proxies; all module dependencies are already used by the SDK's own tests.
func command(t *testing.T, dir, name string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, b)
	}
	return b
}

func standalone(t *testing.T, kind string) string {
	t.Helper()
	root, dir := repoRoot(t), t.TempDir()
	files := []string{"main.go", "provider.go", "session.go"}
	if kind == "echo-router" {
		files = []string{"main.go", "router.go", "rules.go"}
	}
	for _, file := range append(files, "arvel.json") {
		write(t, filepath.Join(dir, file), read(t, filepath.Join(root, "examples", kind, file)))
	}
	copyTree(t, filepath.Join(root, "examples", kind, "conformance"), filepath.Join(dir, "conformance"))
	mod := strings.Replace(string(read(t, filepath.Join(root, "go.mod"))), "module github.com/arvel-dev/arvel-sdk", "module example.invalid/plugin-smoke", 1)
	mod += "\nrequire github.com/arvel-dev/arvel-sdk v0.0.0\nreplace github.com/arvel-dev/arvel-sdk => " + strconv.Quote(root) + "\n"
	write(t, filepath.Join(dir, "go.mod"), []byte(mod))
	write(t, filepath.Join(dir, "go.sum"), read(t, filepath.Join(root, "go.sum")))
	guide := "WRITING-A-PLUGIN.md"
	if kind == "echo-router" {
		guide = "WRITING-A-ROUTER.md"
	}
	blocks := regexp.MustCompile("(?s)```go\n(.*?)```").FindAllSubmatch(markdown(t, filepath.Join(root, "docs", guide)), -1)
	if len(blocks) == 0 {
		t.Fatal("guide has no runnable Go test")
	}
	write(t, filepath.Join(dir, "guide_test.go"), blocks[len(blocks)-1][1])
	return dir
}

func TestGuides_StandalonePluginsPassRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone modules and binaries")
	}
	root := repoRoot(t)
	checker := filepath.Join(t.TempDir(), "arvel-plugin-check.exe")
	command(t, root, "go", "build", "-o", checker, "./cmd/arvel-plugin-check")
	for _, kind := range []string{"echo", "echo-router"} {
		t.Run(kind, func(t *testing.T) {
			dir := standalone(t, kind)
			command(t, dir, "go", "test", "-mod=mod", "./...")
			bin := filepath.Join(dir, "plugin.exe")
			command(t, dir, "go", "build", "-mod=mod", "-o", bin, ".")
			b := command(t, dir, checker, "--json", "--level", "release", "--manifest", "arvel.json", "--scenarios", "conformance", "--", "."+string(filepath.Separator)+filepath.Base(bin))
			var report struct {
				Failed int `json:"failed"`
				Passed int `json:"passed"`
			}
			if err := json.Unmarshal(b, &report); err != nil {
				t.Fatal(err)
			}
			if report.Failed != 0 || report.Passed == 0 {
				t.Fatalf("bad conformance report: %s", b)
			}
		})
	}
}

func TestTemplates_ManifestAndTeamFilesAreValid(t *testing.T) {
	root := repoRoot(t)
	for _, path := range []string{"templates/arvel.json", "examples/team-sample/arvel.json"} {
		m, err := manifest.LoadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if problems := m.ValidateWith(manifest.Options{Official: true}); len(problems) != 0 {
			t.Fatal(problems)
		}
		if m.Type == manifest.TypeTeam {
			if problems := m.ValidateFiles(filepath.Dir(filepath.Join(root, path))); len(problems) != 0 {
				t.Fatal(problems)
			}
		}
	}
}

func TestDocumentation_LocalLinksAndWorkflowSyntax(t *testing.T) {
	root := repoRoot(t)
	paths := []string{"README.md", "docs/WRITING-A-PLUGIN.md", "docs/WRITING-A-ROUTER.md", "docs/WRITING-A-TEAM-PLUGIN.md", "templates/README.plugin.md", "templates/README.router.md", "templates/README.team.md"}
	re := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, path := range paths {
		for _, m := range re.FindAllSubmatch(read(t, filepath.Join(root, path)), -1) {
			link := strings.SplitN(string(m[1]), "#", 2)[0]
			if link == "" || strings.Contains(link, "://") {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, filepath.Dir(path), link)); err != nil {
				t.Errorf("%s: broken link %q", path, link)
			}
		}
	}
	for _, dir := range []string{".github/workflows", "templates/.github/workflows"} {
		files, err := filepath.Glob(filepath.Join(root, dir, "*.yml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			var workflow map[string]any
			if err := yaml.Unmarshal(read(t, file), &workflow); err != nil {
				t.Errorf("%s: %v", file, err)
			}
		}
	}
}

// TestPackaging is opt-in because it requires the pinned GoReleaser executable.
// CI runs both cases on Linux, without publishing or passing a release token.
func TestPackaging(t *testing.T) {
	if os.Getenv("ARVEL_PACKAGING") != "1" {
		t.Skip("set ARVEL_PACKAGING=1 with GoReleaser 2.19.0 installed")
	}
	if runtime.GOOS == "windows" {
		t.Skip("release hooks use POSIX sh and jq on Linux/macOS")
	}
	tool, err := exec.LookPath("goreleaser")
	if err != nil {
		t.Fatal(err)
	}
	for _, plugin := range []bool{false, true} {
		name := "sdk"
		if plugin {
			name = "provider-template"
		}
		t.Run(name, func(t *testing.T) {
			root, dir := repoRoot(t), repoRoot(t)
			configPath := filepath.Join(root, ".goreleaser.yaml")
			project := "arvel-plugin-check"
			var original []byte
			if plugin {
				dir = standalone(t, "echo")
				project = "arvel-plugin-example"
				configPath = filepath.Join(root, "templates", ".goreleaser.yaml")
				write(t, filepath.Join(dir, "arvel.json"), read(t, filepath.Join(root, "templates", "arvel.json")))
				write(t, filepath.Join(dir, "LICENSE"), read(t, filepath.Join(root, "templates", "LICENSE")))
				copyTree(t, filepath.Join(root, "templates", "scripts"), filepath.Join(dir, "scripts"))
				original = read(t, filepath.Join(dir, "arvel.json"))
				// Populate indirect requirements offline before GoReleaser builds.
				command(t, dir, "go", "test", "-mod=mod", "./...")
				command(t, dir, "git", "init")
				// SCM metadata is needed by `check`; snapshots never contact it.
				command(t, dir, "git", "remote", "add", "origin", "https://github.com/example/plugin-smoke.git")
				command(t, dir, "git", "add", ".")
				noHooks := t.TempDir()
				command(t, dir, "git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath="+noHooks, "commit", "-m", "test: standalone packaging fixture")
			}
			var config map[string]any
			if err := yaml.Unmarshal(read(t, configPath), &config); err != nil {
				t.Fatal(err)
			}
			dist := filepath.Join(t.TempDir(), "dist")
			config["dist"] = dist
			b, err := yaml.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			configPath = filepath.Join(t.TempDir(), "goreleaser.yaml")
			write(t, configPath, b)
			command(t, dir, tool, "check", "--config", configPath)
			command(t, dir, tool, "release", "--snapshot", "--config", configPath)
			var manifestBytes []byte
			if plugin {
				manifestBytes = read(t, filepath.Join(dir, ".release", "arvel.json"))
				if bytes.Equal(manifestBytes, original) {
					t.Fatal("manifest version was not substituted")
				}
				if !bytes.Equal(original, read(t, filepath.Join(dir, "arvel.json"))) {
					t.Fatal("source manifest changed")
				}
				m, err := manifest.Load(bytes.NewReader(manifestBytes))
				if err != nil {
					t.Fatal(err)
				}
				if ps := m.Validate(); len(ps) != 0 {
					t.Fatal(ps)
				}
			}
			verifyArtifacts(t, dir, dist, project, manifestBytes)
		})
	}
}

func verifyArtifacts(t *testing.T, dir, dist, project string, manifestBytes []byte) {
	t.Helper()
	expected := map[string]bool{}
	for _, osName := range []string{"darwin", "linux", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			ext := ".tar.gz"
			if osName == "windows" {
				ext = ".zip"
			}
			name := project + "_" + osName + "_" + arch + ext
			expected[name] = true
			files := archiveContents(t, filepath.Join(dist, name))
			binary := project
			if osName == "windows" {
				binary += ".exe"
			}
			if len(files[binary]) == 0 {
				t.Fatalf("%s lacks %s", name, binary)
			}
			if manifestBytes != nil && !bytes.Equal(files["arvel.json"], manifestBytes) {
				t.Fatalf("%s manifest differs", name)
			}
			if manifestBytes == nil && files["arvel.json"] != nil {
				t.Fatal("SDK archive contains a plugin manifest")
			}
		}
	}
	if manifestBytes != nil {
		expected["arvel.json"] = true
	}
	for _, line := range strings.Split(strings.TrimSpace(string(read(t, filepath.Join(dist, "checksums.txt")))), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !expected[fields[1]] {
			t.Fatalf("unexpected checksum line %q", line)
		}
		path := filepath.Join(dist, fields[1])
		if fields[1] == "arvel.json" {
			path = filepath.Join(dir, ".release", "arvel.json")
		}
		digest := sha256.Sum256(read(t, path))
		if hex.EncodeToString(digest[:]) != fields[0] {
			t.Fatalf("bad checksum for %s", fields[1])
		}
		delete(expected, fields[1])
	}
	if len(expected) != 0 {
		t.Fatalf("missing checksums: %v", expected)
	}
}

func archiveContents(t *testing.T, path string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	if strings.HasSuffix(path, ".zip") {
		r, err := zip.OpenReader(path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		for _, f := range r.File {
			rd, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			files[f.Name], err = io.ReadAll(rd)
			_ = rd.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		return files
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		files[h.Name], err = io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestREADMETemplates_PrivacyAndProviderSections(t *testing.T) {
	root := repoRoot(t)
	protocol := string(markdown(t, filepath.Join(root, "docs/PROTOCOL.md")))
	parts := strings.SplitN(protocol, "### 14.2 What the host sends (privacy contract)\n\n", 2)
	if len(parts) != 2 {
		t.Fatal("protocol has no routing privacy section")
	}
	privacy := strings.SplitN(parts[1], "\n\n", 2)[0]
	if !strings.Contains(string(markdown(t, filepath.Join(root, "templates/README.router.md"))), privacy) {
		t.Fatal("router privacy contract drifted")
	}
	provider := string(markdown(t, filepath.Join(root, "templates/README.plugin.md")))
	for _, section := range []string{"What you need", "Connections", "Signing in", "Models & reasoning effort", "Permissions & safety", "Usage & limits", "Troubleshooting", "Privacy", "Compatibility", "Disclaimer", "Development", "License"} {
		if !strings.Contains(provider, "## "+section+"\n") {
			t.Errorf("provider template lacks %s", section)
		}
	}
}

// Keep build tools checked explicitly rather than silently accepting a different
// release tool in the opt-in packaging job.
func TestPackagingToolVersion(t *testing.T) {
	if os.Getenv("ARVEL_PACKAGING") != "1" {
		t.Skip("packaging job only")
	}
	b := command(t, repoRoot(t), "goreleaser", "--version")
	if !strings.Contains(string(b), "2.19.0") {
		t.Fatalf("unexpected GoReleaser version: %s", b)
	}
}
