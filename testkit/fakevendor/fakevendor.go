// SPDX-License-Identifier: Apache-2.0

package fakevendor

import (
	"context"
	"fmt"
	"os"
)

// Main is the entry point of the standalone `fakevendor` CLI: it loads the
// script at args[1] and replays it with args[2:] as argv. It is used for manual
// debugging of a script; Build compiles self-contained binaries for scenarios.
func Main(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fakevendor <script.yaml> [argv...]")
		return 2
	}
	s, err := LoadFile(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return Run(context.Background(), s, args[2:], os.Stdin, os.Stdout, os.Stderr)
}
