// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"
)

func TestCheckConcurrency_EchoPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the example binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/echo", "echo")
	f := &flags{binary: bin}
	if c := checkConcurrency(context.Background(), f); c.Status != StatusPass {
		t.Fatalf("concurrency: status=%s detail=%s", c.Status, c.Detail)
	}
}

// Echo has no long-running turn, so cancel and busy cannot be strictly
// asserted: they may only skip, never fail.
func TestCheckCancelBusy_EchoSkips(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the example binary")
	}
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/examples/echo", "echo")
	f := &flags{binary: bin}
	if c := checkCancel(context.Background(), f); c.Status == StatusFail {
		t.Fatalf("cancel: status=%s detail=%s", c.Status, c.Detail)
	}
	if c := checkBusy(context.Background(), f); c.Status == StatusFail {
		t.Fatalf("busy: status=%s detail=%s", c.Status, c.Detail)
	}
}

// The block-prompt stub keeps a turn running until it is cancelled, so both the
// cancel and busy checks pass strictly.
func TestCheckCancelBusy_StubPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("go build of the stub binary")
	}
	t.Setenv("STUB_MODE", "block-prompt")
	bin := buildBinary(t, "github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check/testdata/stubprovider", "stub")
	f := &flags{binary: bin}
	if c := checkCancel(context.Background(), f); c.Status != StatusPass {
		t.Fatalf("cancel: status=%s detail=%s", c.Status, c.Detail)
	}
	if c := checkBusy(context.Background(), f); c.Status != StatusPass {
		t.Fatalf("busy: status=%s detail=%s", c.Status, c.Detail)
	}
}
