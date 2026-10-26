// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func parse(env []string) map[string]string {
	m := map[string]string{}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestEnv_AllowListOnly(t *testing.T) {
	b := newEnv("linux", []string{
		"PATH=/bin", "HOME=/h", "LANG=C", "LC_ALL=C", "XDG_DATA_HOME=/x", "TMPDIR=/t",
		"ANTHROPIC_API_KEY=secret", "GITHUB_TOKEN=t", "SSH_AUTH_SOCK=/s", "GIT_ASKPASS=a", "GH_TOKEN=g",
		"FOO=bar", "TERM=xterm", "noequals",
	})
	got := parse(b.Build())
	want := []string{"HOME", "LANG", "LC_ALL", "PATH", "TERM", "TMPDIR", "XDG_DATA_HOME"}
	if !reflect.DeepEqual(keys(got), want) {
		t.Fatalf("keys = %v, want %v", keys(got), want)
	}
	if got["TERM"] != "dumb" {
		t.Fatalf("TERM = %q", got["TERM"])
	}
}

func TestEnv_WindowsCaseInsensitive(t *testing.T) {
	b := newEnv("windows", []string{`Path=C:\x`, `SystemRoot=C:\Windows`, `USERPROFILE=C:\u`, "github_token=t", `=C:=C:\dir`})
	got := parse(b.Build())
	if got["Path"] != `C:\x` || got["SystemRoot"] != `C:\Windows` || got["USERPROFILE"] != `C:\u` {
		t.Fatalf("env = %v", got)
	}
	if _, ok := got["github_token"]; ok {
		t.Fatal("token inherited")
	}
	b.Set("PATH", "y")
	got = parse(b.Build())
	if got["PATH"] != "y" {
		t.Fatalf("PATH not replaced: %v", got)
	}
	if _, ok := got["Path"]; ok {
		t.Fatalf("both spellings present: %v", got)
	}
}

func TestEnv_SetNeverPass(t *testing.T) {
	b := newEnv("linux", nil)
	for _, n := range NeverPass() {
		b.Set(n, "x")
	}
	w := newEnv("windows", nil)
	w.Set("github_token", "x")
	if _, ok := parse(w.Build())["github_token"]; ok {
		t.Fatal("windows case variant passed")
	}
	b.Set("FOO", "bar").Set("FOO", "baz")
	got := parse(b.Build())
	for _, n := range NeverPass() {
		if _, ok := got[n]; ok {
			t.Errorf("%s passed", n)
		}
	}
	if got["FOO"] != "baz" {
		t.Fatalf("FOO = %q", got["FOO"])
	}
}

func TestEnv_SetInvalidIgnored(t *testing.T) {
	b := newEnv("linux", nil)
	b.Set("", "v").Set("A=B", "v").Set("A\x00", "v").Set("OK", "v\x00")
	if got := parse(b.Build()); len(got) != 1 { // TERM only
		t.Fatalf("env = %v", got)
	}
}

func TestEnv_Strip(t *testing.T) {
	base := []string{"XDG_A=1", "LC_ALL=C", "PATH=/bin"}
	got := parse(newEnv("linux", base).Strip("XDG_", "LC_").Build())
	if _, ok := got["XDG_A"]; ok {
		t.Fatal("XDG_A kept")
	}
	if _, ok := got["LC_ALL"]; ok {
		t.Fatal("LC_ALL kept")
	}
	if got["PATH"] != "/bin" {
		t.Fatal("PATH lost")
	}
	if got := parse(newEnv("linux", base).Strip("").Build()); len(got) != 4 {
		t.Fatalf("empty prefix stripped: %v", got)
	}
	got = parse(newEnv("linux", nil).Strip("ANTHROPIC_").Set("ANTHROPIC_API_KEY", "k").Build())
	if got["ANTHROPIC_API_KEY"] != "k" {
		t.Fatal("Set after Strip lost")
	}
	got = parse(newEnv("linux", nil).Set("ANTHROPIC_API_KEY", "k").Strip("ANTHROPIC_").Build())
	if _, ok := got["ANTHROPIC_API_KEY"]; ok {
		t.Fatal("Strip after Set kept the variable")
	}
	got = parse(newEnv("windows", nil).Set("Anthropic_Key", "k").Strip("anthropic_").Build())
	if _, ok := got["Anthropic_Key"]; ok {
		t.Fatal("windows Strip is case-sensitive")
	}
}

func TestEnv_Proxy(t *testing.T) {
	const p = "http://127.0.0.1:41234"
	if got := newEnv("linux", nil).Proxy("").Build(); len(got) != 1 {
		t.Fatalf("empty proxy changed env: %v", got)
	}
	got := parse(newEnv("linux", nil).Proxy(p).Build())
	for _, k := range []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "https_proxy", "http_proxy", "all_proxy"} {
		if got[k] != p {
			t.Errorf("%s = %q", k, got[k])
		}
	}
	for _, k := range []string{"NO_PROXY", "no_proxy"} {
		if got[k] != "localhost,127.0.0.1" {
			t.Errorf("%s = %q", k, got[k])
		}
	}
	if len(got) != 9 { // 8 proxy variables + TERM
		t.Errorf("variables = %v", keys(got))
	}
	w := newEnv("windows", nil).Proxy(p).Build()
	n := 0
	for _, e := range w {
		if strings.EqualFold(strings.SplitN(e, "=", 2)[0], "HTTPS_PROXY") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("windows HTTPS_PROXY count = %d", n)
	}
}

func TestEnv_BuildSortedAndFresh(t *testing.T) {
	b := newEnv("linux", []string{"PATH=/bin", "HOME=/h"}).Set("ZED", "1").Set("ALPHA", "2")
	first := b.Build()
	if !sort.StringsAreSorted(first) {
		t.Fatalf("not sorted: %v", first)
	}
	first[0] = "MUTATED=1"
	if second := b.Build(); second[0] == "MUTATED=1" {
		t.Fatal("Build shares its slice")
	}
}

func TestEnv_RealEnvironment(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "x")
	t.Setenv("PROCX_TEST_SECRET", "y")
	got := parse(Env().Build())
	if _, ok := got["GITHUB_TOKEN"]; ok {
		t.Error("GITHUB_TOKEN inherited")
	}
	if _, ok := got["PROCX_TEST_SECRET"]; ok {
		t.Error("PROCX_TEST_SECRET inherited")
	}
	if got["TERM"] != "dumb" {
		t.Errorf("TERM = %q", got["TERM"])
	}
}

func TestNeverPass(t *testing.T) {
	got := NeverPass()
	want := []string{"SSH_AUTH_SOCK", "GIT_ASKPASS", "GH_TOKEN", "GITHUB_TOKEN"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	got[0] = "MUTATED"
	if NeverPass()[0] != "SSH_AUTH_SOCK" {
		t.Fatal("NeverPass shares its array")
	}
}
