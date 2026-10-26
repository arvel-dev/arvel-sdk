// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"errors"
	"strings"
	"testing"
)

func TestQuoteCmdArg(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo", "foo"},
		{"--model=sonnet", `"--model=sonnet"`},
		{"a b", `"a b"`},
		{"", `""`},
		{"a&b", `"a&b"`},
		{"a|b", `"a|b"`},
		{"a<b", `"a<b"`},
		{"a>b", `"a>b"`},
		{"a^b", `"a^b"`},
		{"(x)", `"(x)"`},
		{"a,b", `"a,b"`},
		{"a;b", `"a;b"`},
		{"a\tb", "\"a\tb\""},
		{`C:\dir\`, `C:\dir\`},
		{`C:\my dir\`, `"C:\my dir\\"`},
		{`C:\my dir\\`, `"C:\my dir\\\\"`},
		{"привет мир", `"привет мир"`},
		{"ü", "ü"},
	}
	for _, c := range cases {
		got, err := quoteCmdArg(c.in)
		if err != nil || got != c.want {
			t.Errorf("quoteCmdArg(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestQuoteCmdArg_Rejects(t *testing.T) {
	for _, in := range []string{`say "hi"`, "100%", "%PATH%", "wow!", "a\nb", "a\rb", "a\x00b"} {
		_, err := quoteCmdArg(in)
		if !errors.Is(err, ErrUnsafeArg) {
			t.Errorf("quoteCmdArg(%q) err = %v", in, err)
			continue
		}
		if strings.Contains(err.Error(), in) {
			t.Errorf("error echoes the argument: %v", err)
		}
	}
}

func TestCmdShimLine(t *testing.T) {
	const cmdExe = `C:\Windows\System32\cmd.exe`
	got, err := cmdShimLine(cmdExe, `C:\npm\claude.cmd`, []string{"--version", "two words"})
	want := `"C:\Windows\System32\cmd.exe" /d /s /c ""C:\npm\claude.cmd" --version "two words""`
	if err != nil || got != want {
		t.Fatalf("got %q, %v\nwant %q", got, err, want)
	}
	got, err = cmdShimLine(cmdExe, `C:\my tools\x.cmd`, nil)
	want = `"C:\Windows\System32\cmd.exe" /d /s /c ""C:\my tools\x.cmd""`
	if err != nil || got != want {
		t.Fatalf("got %q, %v\nwant %q", got, err, want)
	}
	if _, err := cmdShimLine(cmdExe, `C:\100%\x.cmd`, nil); !errors.Is(err, ErrUnsafeArg) {
		t.Errorf("percent in shim path: %v", err)
	}
	if _, err := cmdShimLine(cmdExe, `C:\x.cmd`, []string{"a%"}); !errors.Is(err, ErrUnsafeArg) {
		t.Errorf("percent in arg: %v", err)
	}
	if _, err := cmdShimLine(`C:\"bad\cmd.exe`, `C:\x.cmd`, nil); err == nil {
		t.Error("quote in cmd.exe path accepted")
	}
}

func TestIsCmdShim(t *testing.T) {
	for in, want := range map[string]bool{
		"a.cmd": true, "A.CMD": true, "a.bat": true, `C:\x\y.Bat`: true,
		"a.exe": false, "cmd": false, "a.cmd.exe": false, "": false,
	} {
		if got := isCmdShim(in); got != want {
			t.Errorf("isCmdShim(%q) = %v, want %v", in, got, want)
		}
	}
}
