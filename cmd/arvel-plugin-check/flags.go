// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"io"
)

// flags are the parsed command-line options of arvel-plugin-check.
type flags struct {
	level     string   // basic or release
	scenarios string   // directory of scenario files (release level)
	manifest  string   // path to arvel.json
	pkg       string   // plugin package directory (declarative --manifest mode)
	json      bool     // print the report as JSON
	binary    string   // plugin binary (empty in declarative mode)
	args      []string // arguments passed to the plugin binary
}

// parseFlags parses args with the stdlib flag package. Everything after the
// "--" separator (or after the first non-flag argument) is the plugin binary
// and its arguments.
func parseFlags(args []string, stderr io.Writer) (*flags, error) {
	f := &flags{}
	fs := flag.NewFlagSet("arvel-plugin-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.level, "level", "basic", "conformance level: basic or release")
	fs.StringVar(&f.scenarios, "scenarios", "./conformance", "directory of scenario files (release level)")
	fs.StringVar(&f.manifest, "manifest", "arvel.json", "path to arvel.json")
	fs.StringVar(&f.pkg, "package", "", "plugin package directory (declarative --manifest mode)")
	fs.BoolVar(&f.json, "json", false, "print the report as JSON")
	fs.Usage = func() { usage(fs, stderr) }
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if rest := fs.Args(); len(rest) > 0 {
		f.binary = rest[0]
		if len(rest) > 1 {
			f.args = rest[1:]
		}
	}
	return f, nil
}

// usage writes the command help to w.
func usage(fs *flag.FlagSet, w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  arvel-plugin-check [flags] -- <plugin binary> [args]")
	fmt.Fprintln(w, "  arvel-plugin-check --manifest arvel.json --package <dir> [flags]")
	fs.PrintDefaults()
}
