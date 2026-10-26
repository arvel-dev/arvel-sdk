// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"regexp"
	"strings"
)

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// validName reports whether s is a safe single path element for a skill folder.
func validName(s string) bool { return nameRE.MatchString(s) && !strings.Contains(s, "..") }

func trimEOL(s string) string { return strings.TrimRight(s, "\r\n") }

// rewriteFrontMatter edits the front matter of a SKILL.md. When name is not
// empty the top-level "name:" field is set to it (the folder was renamed and the
// Agent Skills format requires the two to match). When manual is true the
// top-level "disable-model-invocation:" field is set to true. The body and all
// other fields are kept byte for byte, and CRLF line endings are preserved.
func rewriteFrontMatter(src []byte, name string, manual bool) []byte {
	if name == "" && !manual {
		return src
	}
	text := string(src)
	lines := strings.SplitAfter(text, "\n")
	eol := "\n"
	if len(lines) > 0 && strings.HasSuffix(lines[0], "\r\n") {
		eol = "\r\n"
	}
	var want []string
	if name != "" {
		want = append(want, "name: "+name)
	}
	if manual {
		want = append(want, "disable-model-invocation: true")
	}

	closing := -1
	if len(lines) > 1 && trimEOL(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if trimEOL(lines[i]) == "---" {
				closing = i
				break
			}
		}
	}
	if closing < 0 { // no (complete) front matter: add one
		var b strings.Builder
		b.WriteString("---" + eol)
		for _, w := range want {
			b.WriteString(w + eol)
		}
		b.WriteString("---" + eol)
		b.WriteString(text)
		return []byte(b.String())
	}

	var b strings.Builder
	b.WriteString(lines[0])
	done := map[string]bool{}
	for _, l := range lines[1:closing] {
		c := trimEOL(l)
		switch {
		case name != "" && strings.HasPrefix(c, "name:"):
			b.WriteString("name: " + name + eol)
			done["name"] = true
		case manual && strings.HasPrefix(c, "disable-model-invocation:"):
			b.WriteString("disable-model-invocation: true" + eol)
			done["manual"] = true
		default:
			b.WriteString(l)
		}
	}
	if name != "" && !done["name"] {
		b.WriteString("name: " + name + eol)
	}
	if manual && !done["manual"] {
		b.WriteString("disable-model-invocation: true" + eol)
	}
	for _, l := range lines[closing:] {
		b.WriteString(l)
	}
	return []byte(b.String())
}
