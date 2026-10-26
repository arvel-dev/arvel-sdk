// SPDX-License-Identifier: Apache-2.0

package skills

import "testing"

func TestRewriteFrontMatter(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		rename string
		manual bool
		want   string
	}{
		{"manual_adds_line", "---\nname: a\n---\nbody\n", "", true,
			"---\nname: a\ndisable-model-invocation: true\n---\nbody\n"},
		{"manual_replaces_existing_false", "---\nname: a\ndisable-model-invocation: false\n---\nbody\n", "", true,
			"---\nname: a\ndisable-model-invocation: true\n---\nbody\n"},
		{"manual_keeps_other_fields", "---\nname: a\ndescription: does things\n---\nx\n", "", true,
			"---\nname: a\ndescription: does things\ndisable-model-invocation: true\n---\nx\n"},
		{"rename_replaces_name", "---\nname: pest\ndescription: d\n---\nx\n", "arvel-pest", false,
			"---\nname: arvel-pest\ndescription: d\n---\nx\n"},
		{"rename_adds_missing_name", "---\ndescription: d\n---\nx\n", "arvel-pest", false,
			"---\ndescription: d\nname: arvel-pest\n---\nx\n"},
		{"rename_and_manual", "---\nname: a\n---\nx\n", "arvel-a", true,
			"---\nname: arvel-a\ndisable-model-invocation: true\n---\nx\n"},
		{"crlf_preserved", "---\r\nname: a\r\n---\r\nbody\r\n", "", true,
			"---\r\nname: a\r\ndisable-model-invocation: true\r\n---\r\nbody\r\n"},
		{"no_front_matter_manual_prepends", "just text\n", "", true,
			"---\ndisable-model-invocation: true\n---\njust text\n"},
		{"no_front_matter_noop", "just text\n", "", false, "just text\n"},
		{"unclosed_front_matter_treated_as_none", "---\nname: a\nbody\n", "", true,
			"---\ndisable-model-invocation: true\n---\n---\nname: a\nbody\n"},
		{"body_untouched", "---\nname: a\n---\n---\nname: not-a-field\n", "arvel-a", true,
			"---\nname: arvel-a\ndisable-model-invocation: true\n---\n---\nname: not-a-field\n"},
		{"indented_name_not_touched", "---\nmeta:\n  name: inner\n---\nx\n", "arvel-a", false,
			"---\nmeta:\n  name: inner\nname: arvel-a\n---\nx\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(rewriteFrontMatter([]byte(tc.in), tc.rename, tc.manual))
			if got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestValidName(t *testing.T) {
	good := []string{"a", "pest-tests", "My_Skill.v2", "x1"}
	bad := []string{"", "..", "a/b", `a\b`, ".hidden", "-lead", "a..b", "has space"}
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'a'
	}
	bad = append(bad, string(long))
	for _, s := range good {
		if !validName(s) {
			t.Errorf("validName(%q) = false", s)
		}
	}
	for _, s := range bad {
		if validName(s) {
			t.Errorf("validName(%q) = true", s)
		}
	}
}
