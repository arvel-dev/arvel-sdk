// SPDX-License-Identifier: MIT-0

// Command echo-router is a minimal Arvel router plugin: deterministic keyword
// rules, no network, no secrets. It is the SDK's router test subject and a
// template for router plugins.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/arvel-dev/arvel-sdk/plugin"
)

var version = "0.1.0" // overwritten by -ldflags at release

func main() {
	if err := plugin.RunRouter(context.Background(), router{}, plugin.WithVersion("arvel-plugin-echo-router", version)); err != nil {
		fmt.Fprintln(os.Stderr, err) // stderr only: stdout is the protocol channel
		os.Exit(1)
	}
}
