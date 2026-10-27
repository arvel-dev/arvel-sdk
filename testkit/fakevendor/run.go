// SPDX-License-Identifier: Apache-2.0

package fakevendor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"time"

	"github.com/arvel-dev/arvel-sdk/testkit/scenario"
)

// Run executes the script against argv/stdin/stdout/stderr and returns the exit
// code. It answers --version and auth status before the steps. A mismatch or a
// missing stdin line writes a diagnostic to stderr and returns 2.
func Run(ctx context.Context, s *Script, argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if slices.Contains(argv, "--version") && s.Version != "" {
		fmt.Fprintln(stdout, s.Version)
		return 0
	}
	if slices.Equal(argv, []string{"auth", "status"}) && s.AuthStatus != "" {
		fmt.Fprintln(stdout, s.AuthStatus)
		return 0
	}

	var scan *bufio.Scanner
	readLine := func() (string, bool) {
		if scan == nil {
			scan = bufio.NewScanner(stdin)
		}
		if scan.Scan() {
			return scan.Text(), true
		}
		return "", false
	}

	for i, st := range s.Steps {
		switch {
		case st.Expect != nil:
			if !runExpect(st.Expect, argv, readLine, stderr) {
				fmt.Fprintf(stderr, "fakevendor: step %d expectation failed\n", i)
				return 2
			}
		case st.Emit != nil:
			w := stdout
			if st.Emit.Stream == "stderr" {
				w = stderr
			}
			fmt.Fprintln(w, st.Emit.Line)
		case st.SleepMillis > 0:
			t := time.NewTimer(time.Duration(st.SleepMillis) * time.Millisecond)
			select {
			case <-ctx.Done():
				t.Stop()
				return 0
			case <-t.C:
			}
		case st.ExitCode != nil:
			return *st.ExitCode
		}
	}
	return 0
}

func runExpect(e *Expect, argv []string, readLine func() (string, bool), stderr io.Writer) bool {
	if e.Argv != nil {
		return slices.Equal(argv, e.Argv)
	}
	line, ok := readLine()
	if !ok {
		fmt.Fprintln(stderr, "fakevendor: expected a stdin line but stdin ended")
		return false
	}
	if e.Regex != "" {
		re := regexp.MustCompile(e.Regex) // already validated by Validate
		return re.MatchString(line)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		fmt.Fprintf(stderr, "fakevendor: stdin line is not JSON: %q\n", line)
		return false
	}
	return scenario.Subset(e.JSON, decoded)
}
