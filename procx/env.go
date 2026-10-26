// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"os"
	"runtime"
	"sort"
	"strings"
)

// neverPass lists variables that must never reach a child process, whatever
// the caller sets (PROTOCOL §15.4).
var neverPass = [...]string{"SSH_AUTH_SOCK", "GIT_ASKPASS", "GH_TOKEN", "GITHUB_TOKEN"}

// NeverPass returns the names of the variables EnvBuilder never passes on.
func NeverPass() []string {
	out := make([]string, len(neverPass))
	copy(out, neverPass[:])
	return out
}

var allowedExact = map[string]bool{
	"PATH": true, "HOME": true, "USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true,
	"SYSTEMROOT": true, "TEMP": true, "TMP": true, "TMPDIR": true, "LANG": true,
}

var allowedPrefix = [...]string{"LC_", "XDG_"}

func allowed(norm string) bool {
	if allowedExact[norm] {
		return true
	}
	for _, p := range allowedPrefix {
		if strings.HasPrefix(norm, p) {
			return true
		}
	}
	return false
}

type envVar struct{ name, value string }

// EnvBuilder builds the environment of a child process. It starts from a small
// allow-list of the parent environment, so credentials and tokens in the
// parent are not inherited. The methods return the builder for chaining.
type EnvBuilder struct {
	goos string
	vars map[string]envVar // key: normKey(name)
}

// Env returns a builder holding the allow-listed part of the current
// environment (PATH, HOME, USERPROFILE, APPDATA, LOCALAPPDATA, SystemRoot,
// TEMP, TMP, TMPDIR, LANG, LC_* and XDG_*) and TERM=dumb.
func Env() *EnvBuilder { return newEnv(runtime.GOOS, os.Environ()) }

func newEnv(goos string, environ []string) *EnvBuilder {
	b := &EnvBuilder{goos: goos, vars: map[string]envVar{}}
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue // Windows keeps hidden "=C:=C:\dir" entries
		}
		if allowed(b.normKey(name)) {
			b.vars[b.normKey(name)] = envVar{name, value}
		}
	}
	b.put("TERM", "dumb")
	return b
}

// normKey folds names to upper case on Windows, where they are case-insensitive.
func (b *EnvBuilder) normKey(name string) string {
	if b.goos == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func (b *EnvBuilder) put(name, value string) {
	b.vars[b.normKey(name)] = envVar{name, value}
}

// Set adds or replaces a variable. The call is ignored when the name is empty
// or contains "=" or NUL, when the value contains NUL, or when the name is on
// the never-pass list (see NeverPass).
func (b *EnvBuilder) Set(name, value string) *EnvBuilder {
	if name == "" || strings.ContainsAny(name, "=\x00") || strings.Contains(value, "\x00") {
		return b
	}
	norm := b.normKey(name)
	for _, n := range neverPass {
		if norm == b.normKey(n) {
			return b
		}
	}
	b.put(name, value)
	return b
}

// Strip removes every variable currently held whose name starts with one of
// the prefixes. Empty prefixes are ignored. The order of calls matters: a Set
// after Strip is kept, so the idiom
//
//	procx.Env().Strip("ANTHROPIC_").Set("ANTHROPIC_API_KEY", key)
//
// removes inherited auth variables and then sets the one the connection wants.
func (b *EnvBuilder) Strip(prefixes ...string) *EnvBuilder {
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		np := b.normKey(p)
		for k := range b.vars {
			if strings.HasPrefix(k, np) {
				delete(b.vars, k)
			}
		}
	}
	return b
}

// Proxy points the child at the sandbox egress proxy (PROTOCOL §5.1, §15.3):
// HTTPS_PROXY, HTTP_PROXY and ALL_PROXY, with lower-case twins, and
// NO_PROXY=localhost,127.0.0.1. proxy is the sandbox.proxy value from
// initialize; an empty proxy is a no-op.
func (b *EnvBuilder) Proxy(proxy string) *EnvBuilder {
	if proxy == "" {
		return b
	}
	for _, n := range []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY"} {
		b.put(n, proxy)
		b.put(strings.ToLower(n), proxy)
	}
	b.put("NO_PROXY", "localhost,127.0.0.1")
	b.put("no_proxy", "localhost,127.0.0.1")
	return b
}

// Build returns the environment as "NAME=value" strings sorted by name. Each
// call returns a new slice.
func (b *EnvBuilder) Build() []string {
	keys := make([]string, 0, len(b.vars))
	for k := range b.vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = b.vars[k].name + "=" + b.vars[k].value
	}
	return out
}
