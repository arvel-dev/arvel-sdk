// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/mod/semver"
)

const (
	versionTimeout     = 10 * time.Second
	versionOutputLimit = 64 << 10
	versionRawLimit    = 4 << 10
)

// versionRE finds the first version number in command output (SPEC §5.5).
var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+(?:[-+][\w.]+)?`)

// Version runs path with args (default: --version) and returns the first
// version number found in its stdout, then stderr, without a leading "v". The
// program gets a minimal environment and 10 seconds (less if ctx expires
// sooner); its output is bounded. raw holds the trimmed output, cut to 4 KiB,
// and is returned on every path so callers can show it. A non-zero exit code
// is an error even when the output holds a version; a missing version is an
// error wrapping ErrNoVersion.
func Version(ctx context.Context, path string, args ...string) (ver string, raw string, err error) {
	args = versionArgs(args)
	base := filepath.Base(path)
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	out := &limitedBuffer{max: versionOutputLimit}
	errb := &limitedBuffer{max: versionOutputLimit}
	p, err := Start(ctx, Spec{Path: path, Args: args, Stdout: out, Stderr: errb})
	if err != nil {
		return "", "", fmt.Errorf("procx: version probe of %s: %w", base, err)
	}
	code, werr := p.Wait()
	raw = joinOutput(out.String(), errb.String())

	if cerr := ctx.Err(); cerr != nil {
		return "", raw, fmt.Errorf("procx: version probe of %s: %w", base, cerr)
	}
	if werr != nil {
		return "", raw, fmt.Errorf("procx: version probe of %s: %w", base, werr)
	}
	if code != 0 {
		return "", raw, fmt.Errorf("procx: version probe of %s: exited with code %d", base, code)
	}
	ver = versionRE.FindString(raw)
	if ver == "" {
		return "", raw, fmt.Errorf("procx: version probe of %s: %w", base, ErrNoVersion)
	}
	return ver, raw, nil
}

func versionArgs(args []string) []string {
	if len(args) == 0 {
		return []string{"--version"}
	}
	return args
}

func joinOutput(stdout, stderr string) string {
	var parts []string
	for _, s := range []string{stdout, stderr} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	raw := strings.Join(parts, "\n")
	if len(raw) <= versionRawLimit {
		return raw
	}
	cut := versionRawLimit
	for i := 0; i < utf8.UTFMax-1 && cut > 0 && !utf8.RuneStart(raw[cut]); i++ {
		cut--
	}
	return raw[:cut]
}

// limitedBuffer keeps the first max bytes written and drops the rest. Write
// always reports the full length, so a chatty child never blocks on a short write.
type limitedBuffer struct {
	max int
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		b.buf.Write(p[:room])
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string { return b.buf.String() }

// AtLeast reports whether the version found is at least min, by semver
// precedence. A leading "v" is optional on both sides. An empty min means no
// minimum and gives true; an invalid version on either side gives false. A
// pre-release is lower than its release, and build metadata is ignored.
func AtLeast(found, min string) bool {
	found, min = strings.TrimSpace(found), strings.TrimSpace(min)
	if min == "" {
		return true
	}
	f, m := withV(found), withV(min)
	if !semver.IsValid(f) || !semver.IsValid(m) {
		return false
	}
	return semver.Compare(f, m) >= 0
}

func withV(s string) string {
	if strings.HasPrefix(s, "v") {
		return s
	}
	return "v" + s
}
