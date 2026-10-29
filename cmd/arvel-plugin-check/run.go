// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/arvel-dev/arvel-sdk/manifest"
)

// run is the testable entry point of arvel-plugin-check. It returns the
// process exit code: 0 when every non-skipped check passed, 1 on any failure,
// and 2 on a usage or flag error.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return 2 // parseFlags already printed the error and usage
	}
	if f.level != "basic" && f.level != "release" {
		fmt.Fprintf(stderr, "arvel-plugin-check: invalid --level %q (want basic or release)\n", f.level)
		return 2
	}
	if f.binary == "" && f.pkg == "" {
		fmt.Fprintln(stderr, "arvel-plugin-check: need a plugin binary (after --) or --package")
		return 2
	}

	rep := dispatch(ctx, f)
	if f.json {
		if err := renderJSON(stdout, rep); err != nil {
			fmt.Fprintf(stderr, "arvel-plugin-check: render report: %v\n", err)
			return 2
		}
	} else {
		renderHuman(stdout, rep)
	}
	if rep.ok() {
		return 0
	}
	return 1
}

// dispatch loads the manifest and routes to the provider, router or declarative
// suite based on its type.
func dispatch(ctx context.Context, f *flags) *Report {
	// --package is declarative mode: there is no plugin binary.
	if f.pkg != "" {
		if f.testEnv != "" {
			rep := &Report{Level: f.level}
			rep.add(fail("test environment", "--test-env applies only to provider/router binaries"))
			return rep
		}
		return runDeclarative(ctx, f)
	}

	m, err := manifest.LoadFile(f.manifest)
	if err != nil {
		rep := &Report{Plugin: f.binary, Kind: "", Level: f.level}
		rep.add(fail("manifest", err.Error()))
		return rep
	}

	f.loaded = m
	if f.testEnv != "" {
		if m.Type.Declarative() {
			rep := &Report{Plugin: f.manifest, Kind: string(m.Type), Level: f.level}
			rep.add(fail("test environment", "--test-env applies only to provider/router binaries"))
			return rep
		}
		cfg, err := loadTestConfiguration(f.testEnv)
		if err != nil {
			rep := &Report{Plugin: f.binary, Kind: string(m.Type), Level: f.level}
			rep.add(fail("test environment", err.Error()))
			return rep
		}
		f.config = cfg
	}
	switch m.Type {
	case manifest.TypeProvider:
		return runProvider(ctx, f, m)
	case manifest.TypeRouter:
		return runRouter(ctx, f, m)
	default: // team, integration
		return runDeclarative(ctx, f)
	}
}
