// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/arvel-dev/arvel-sdk/redact"
)

// newCanary returns a random secret injected into initialize to detect leakage.
// The value is long enough for the plugin framework's redactor to register it.
func newCanary() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "arvel-canary-" + hex.EncodeToString(b[:])
}

// checkCanaryLeak verifies the canary never appears in the concatenated plugin
// output. It never prints the canary value, only the fact of a leak.
func checkCanaryLeak(canary, out string) CheckResult {
	if canary == "" {
		return skip("canary leak", "no canary was injected")
	}
	if strings.Contains(out, canary) {
		return fail("canary leak", "the injected canary secret appeared in plugin output")
	}
	return pass("canary leak", "the injected secret did not appear in plugin output")
}

// redactDetails scrubs the secret from every check detail so the report never
// re-prints an injected secret that a plugin echoed into some other check's
// message (for example a stderr line that is not JSON).
func redactDetails(cs []CheckResult, secret string) {
	if secret == "" {
		return
	}
	r := redact.New()
	r.Add(secret)
	for i := range cs {
		cs[i].Detail = r.String(cs[i].Detail)
	}
}
