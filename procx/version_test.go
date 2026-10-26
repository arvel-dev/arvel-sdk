// SPDX-License-Identifier: Apache-2.0

package procx

import "testing"

func TestAtLeast(t *testing.T) {
	cases := []struct {
		found, min string
		want       bool
	}{
		{"2.1.310", "2.1.259", true},
		{"v2.1.310", "2.1.259", true},
		{"2.1.259", "v2.1.259", true},
		{"2.1.258", "2.1.259", false},
		{"2.1.0-beta.1", "2.1.0", false},
		{"2.1.0", "2.1.0-beta.1", true},
		{"1.2.3+build5", "1.2.3", true},
		{"garbage", "1.0.0", false},
		{"1.0.0", "garbage", false},
		{"1.0.0", "", true},
		{" 1.0.0 ", "1.0.0", true},
	}
	for _, c := range cases {
		if got := AtLeast(c.found, c.min); got != c.want {
			t.Errorf("AtLeast(%q, %q) = %v, want %v", c.found, c.min, got, c.want)
		}
	}
}

func TestVersionRE(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v2.1.310 (Claude Code)", "2.1.310"},
		{"codex-cli 0.46.0", "0.46.0"},
		{"1.2.3-beta.1", "1.2.3-beta.1"},
		{"1.2.3+build5", "1.2.3+build5"},
		{"1.2.3-beta.1+build5", "1.2.3-beta.1"}, // the SPEC pattern takes one suffix
		{"version 10.20.30-rc_1", "10.20.30-rc_1"},
		{"no digits", ""},
	}
	for _, c := range cases {
		if got := versionRE.FindString(c.in); got != c.want {
			t.Errorf("versionRE(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
