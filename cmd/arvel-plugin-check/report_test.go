// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRenderHuman(t *testing.T) {
	r := &Report{Plugin: "echo", Kind: "provider", Level: "basic"}
	r.add(pass("manifest", "valid"))
	r.add(fail("models", "duplicate id"))
	r.add(skip("stderr JSON lines", "stderr is empty"))

	var buf bytes.Buffer
	renderHuman(&buf, r)
	out := buf.String()

	for _, want := range []string{"PASS manifest", "FAIL models", "     duplicate id", "SKIP stderr JSON lines", "1 passed, 1 failed, 1 skipped"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderJSON_RoundTrip(t *testing.T) {
	want := &Report{Plugin: "echo", Kind: "provider", Level: "basic"}
	want.add(pass("manifest", "valid"))
	want.add(fail("models", "duplicate id"))
	want.add(skip("canary leak", "no output"))

	var buf bytes.Buffer
	if err := renderJSON(&buf, want); err != nil {
		t.Fatal(err)
	}
	var got Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, *want) {
		t.Fatalf("got %+v, want %+v", got, *want)
	}
}

func TestReport_ok(t *testing.T) {
	r := &Report{}
	r.add(pass("a", ""))
	r.add(skip("b", ""))
	if !r.ok() {
		t.Fatal("a report with no failures should be ok")
	}
	r.add(fail("c", ""))
	if r.ok() {
		t.Fatal("a report with a failure should not be ok")
	}
}
