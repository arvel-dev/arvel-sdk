// SPDX-License-Identifier: MIT-0

// Command echo is a minimal Arvel provider plugin: an in-memory echo with one
// connection kind "local" and one model "echo-1". It is the SDK's provider test
// subject and a template for provider plugins.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/arvel-dev/arvel-sdk/plugin"
)

var version = "0.1.0" // overwritten by -ldflags at release

func main() {
	if err := plugin.Run(context.Background(), newProvider(), plugin.WithVersion("arvel-plugin-echo", version)); err != nil {
		fmt.Fprintln(os.Stderr, err) // stderr only: stdout is the protocol channel
		os.Exit(1)
	}
}
