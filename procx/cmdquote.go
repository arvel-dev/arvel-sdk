// SPDX-License-Identifier: Apache-2.0

package procx

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Windows .cmd and .bat files run only through cmd.exe, whose parser is an
// injection surface for arguments. Arguments are quoted by the Microsoft rules
// (CommandLineToArgvW). Characters that cannot be made safe are rejected
// instead of escaped:
//
//	"      a quote ends cmd.exe's quoted state, so later metacharacters become live
//	%      environment variables are expanded even inside quotes
//	!      delayed expansion can be enabled in the registry
//	CR LF NUL
//
// Pass such content over stdin, in a file, or through the protocol, never in
// the argv of a .cmd or .bat shim.

// isCmdShim reports whether path is a .cmd or .bat file.
func isCmdShim(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
		return true
	}
	return false
}

// quoteCmdArg quotes one argument for a cmd.exe command line. The error wraps
// ErrUnsafeArg and names the character class, never the argument.
func quoteCmdArg(arg string) (string, error) {
	if reason := unsafeReason(arg); reason != "" {
		return "", fmt.Errorf("%w: contains %s", ErrUnsafeArg, reason)
	}
	if arg == "" {
		return `""`, nil
	}
	if !strings.ContainsAny(arg, " \t&|<>^(),;=") {
		return arg, nil
	}
	// Double the trailing backslashes so the closing quote is not escaped.
	trimmed := strings.TrimRight(arg, `\`)
	slashes := len(arg) - len(trimmed)
	return `"` + trimmed + strings.Repeat(`\`, 2*slashes) + `"`, nil
}

func unsafeReason(arg string) string {
	switch {
	case strings.Contains(arg, `"`):
		return "a double quote"
	case strings.Contains(arg, "%"):
		return "a percent sign"
	case strings.Contains(arg, "!"):
		return "an exclamation mark"
	case strings.ContainsAny(arg, "\r\n"):
		return "a line break"
	case strings.Contains(arg, "\x00"):
		return "a NUL byte"
	}
	return ""
}

// cmdShimLine builds the command line that runs a shim through cmd.exe:
//
//	"<cmdExe>" /d /s /c ""<shim>" arg1 arg2"
//
// With /s, cmd.exe strips the first and last quote of the command string, so
// the whole inner command is wrapped in one more pair of quotes.
func cmdShimLine(cmdExe, shim string, args []string) (string, error) {
	if strings.Contains(cmdExe, `"`) {
		return "", fmt.Errorf("procx: cmd.exe path contains a double quote")
	}
	if reason := unsafeReason(shim); reason != "" {
		return "", fmt.Errorf("%w: the shim path contains %s", ErrUnsafeArg, reason)
	}
	inner := `"` + shim + `"`
	for _, a := range args {
		q, err := quoteCmdArg(a)
		if err != nil {
			return "", err
		}
		inner += " " + q
	}
	return `"` + cmdExe + `" /d /s /c "` + inner + `"`, nil
}
