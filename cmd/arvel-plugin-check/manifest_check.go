// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"

	"github.com/arvel-dev/arvel-sdk/manifest"
)

// checkManifest loads and validates arvel.json. It returns a pass with the
// number of warning problems, or a fail listing the error problems.
func checkManifest(path string) CheckResult {
	m, err := manifest.LoadFile(path)
	if err != nil {
		return fail("manifest", err.Error())
	}
	ps := m.ValidateWith(manifest.Options{Official: true})
	var errs []string
	var warns int
	for _, p := range ps {
		if p.Severity == manifest.SeverityError {
			errs = append(errs, p.String())
		} else {
			warns++
		}
	}
	if len(errs) > 0 {
		return fail("manifest", strings.Join(errs, "; "))
	}
	detail := "valid"
	if warns > 0 {
		detail = fmt.Sprintf("valid with %d warning(s)", warns)
	}
	return pass("manifest", detail)
}
