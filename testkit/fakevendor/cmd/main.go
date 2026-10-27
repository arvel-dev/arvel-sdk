// SPDX-License-Identifier: Apache-2.0

// Command fakevendor replays a fake-vendor script from a file. It is useful for
// running a script by hand; scenario runs use fakevendor.Build instead.
package main

import (
	"os"

	"github.com/arvel-dev/arvel-sdk/testkit/fakevendor"
)

func main() { os.Exit(fakevendor.Main(os.Args)) }
