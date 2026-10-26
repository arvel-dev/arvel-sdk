// SPDX-License-Identifier: Apache-2.0

package redact

import "regexp"

// The patterns are immutable compiled values. \b stops words such as
// "task-management-…" from matching "sk-", and the minimum lengths stop
// "sk-ant-x" or "Bearer authentication" from matching.
var (
	pemBlock      = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*KEY(?: BLOCK)?-----.*?-----END [A-Z0-9 ]*KEY(?: BLOCK)?-----`)
	pemUnfinished = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*KEY(?: BLOCK)?-----.*`)
	skAnt         = regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{8,}`)
	skGeneric     = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)
	bearer        = regexp.MustCompile(`(?i)(\bBearer[ \t]+)[A-Za-z0-9._~+/=-]{16,}`)
)

// applyPatterns order matters: complete PEM blocks before an unfinished one,
// and sk-ant- before the generic sk- form.
func applyPatterns(s string) string {
	s = pemBlock.ReplaceAllLiteralString(s, Replacement)
	s = pemUnfinished.ReplaceAllLiteralString(s, Replacement)
	s = skAnt.ReplaceAllLiteralString(s, Replacement)
	s = skGeneric.ReplaceAllLiteralString(s, Replacement)
	return bearer.ReplaceAllString(s, "${1}"+Replacement)
}
