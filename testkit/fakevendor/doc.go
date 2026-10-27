// SPDX-License-Identifier: Apache-2.0

// Package fakevendor builds a fake vendor CLI from a YAML script (SPEC §5.9).
// The fake replays an ordered list of steps: it expects a stdin line that
// matches a JSON subset or a regexp, or asserts argv; it emits a stdout or
// stderr line; it sleeps; it exits with a code. A script may also carry canned
// replies for `--version` and `auth status`.
//
// Build compiles a self-contained executable that replays a script, so a
// provider plugin can spawn it in place of the real vendor CLI. Script files
// that a plugin ships live under testdata/fakevendor/*.yaml and are generated
// from recorded fixtures, never invented.
package fakevendor
