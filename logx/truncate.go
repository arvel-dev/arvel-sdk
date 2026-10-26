// SPDX-License-Identifier: Apache-2.0

package logx

import (
	"strconv"
	"unicode/utf8"
)

// DefaultTruncate is the size Truncate uses when n <= 0: 2 KiB (PROTOCOL §10).
const DefaultTruncate = 2048

// Truncate shortens s to at most n bytes of content, cut at a rune boundary,
// and appends a marker that says how many bytes were removed. The marker is in
// addition to the n bytes. n <= 0 selects DefaultTruncate.
func Truncate(s string, n int) string {
	if n <= 0 {
		n = DefaultTruncate
	}
	if len(s) <= n {
		return s
	}
	cut := n
	for i := 0; i < utf8.UTFMax-1 && cut > 0 && !utf8.RuneStart(s[cut]); i++ {
		cut--
	}
	return s[:cut] + "…[truncated " + strconv.Itoa(len(s)-cut) + " bytes]"
}
