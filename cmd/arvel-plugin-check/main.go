// SPDX-License-Identifier: Apache-2.0

// Command arvel-plugin-check verifies that a plugin conforms to the Arvel
// Provider Protocol (docs/PROTOCOL.md). It runs the basic or release level
// against a provider or router binary, or the --manifest mode against a
// declarative (team/integration) plugin. See SPEC §5.10.
package main

import (
	"context"
	"os"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
