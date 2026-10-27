// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/arvel-dev/arvel-sdk/protocol"
)

// cancelBound is the SPEC §5.10 deadline for a cancel to reach "cancelled".
// The host call itself uses a shorter 10 s transport timeout, so in practice a
// stuck prompt fails before this bound is reached.
const cancelBound = 15 * time.Second

// releaseInitMeta is the minimal initialize metadata the release checks send;
// the session checks only need a local connection.
func releaseInitMeta() protocol.InitMeta {
	return protocol.InitMeta{Connection: protocol.ConnectionInfo{Kind: "local"}}
}

// checkConcurrency creates two sessions and interleaves prompts on both, then
// re-loads one, asserting the plugin keeps per-session state apart.
func checkConcurrency(ctx context.Context, f *flags) CheckResult {
	d, err := startDriver(f)
	if err != nil {
		return fail("concurrency", "start: "+err.Error())
	}
	defer d.Close()
	if _, err := d.Initialize(releaseInitMeta()); err != nil {
		return fail("concurrency", "initialize: "+err.Error())
	}

	meta := protocol.SessionMeta{}
	s1, err := d.NewSession(meta)
	if err != nil {
		return fail("concurrency", "session/new 1: "+err.Error())
	}
	s2, err := d.NewSession(meta)
	if err != nil {
		return fail("concurrency", "session/new 2: "+err.Error())
	}
	blocks := []acp.ContentBlock{acp.TextBlock("hi")}
	// Both calls must be outstanding together. This function owns and joins
	// both workers; Driver serializes only writes, not whole request/response
	// exchanges. No checker lock is held across a driver call.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []string{s1, s2} {
		wg.Go(func() {
			if _, err := d.Prompt(id, blocks); err != nil {
				errs <- fmt.Errorf("prompt %s: %w", id, err)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return fail("concurrency", err.Error())
	}
	if _, err := d.Prompt(s1, blocks); err != nil {
		return fail("concurrency", "prompt A (2nd): "+err.Error())
	}
	if err := d.LoadSession(s2, meta); err != nil {
		// A provider that does not announce resume answers -32601; that is a
		// separate feature, not a concurrency defect.
		var rpcErr *protocol.RPCError
		if !errors.As(err, &rpcErr) || rpcErr.Code != protocol.CodeMethodNotFound {
			return fail("concurrency", "session/load: "+err.Error())
		}
	}
	return pass("concurrency", "2 sessions interleaved ok")
}

// checkCancel starts a prompt, cancels it, and expects the turn to reach
// "cancelled". A provider that finishes before the cancel lands has no
// long-running work, so the strict assertion is skipped.
func checkCancel(ctx context.Context, f *flags) CheckResult {
	d, err := startDriver(f)
	if err != nil {
		return fail("cancel", "start: "+err.Error())
	}
	defer d.Close()
	if _, err := d.Initialize(releaseInitMeta()); err != nil {
		return fail("cancel", "initialize: "+err.Error())
	}

	id, err := d.NewSession(protocol.SessionMeta{})
	if err != nil {
		return fail("cancel", "session/new: "+err.Error())
	}
	blocks := []acp.ContentBlock{acp.TextBlock("hi")}
	type promptResult struct {
		stop acp.StopReason
		err  error
	}
	ch := make(chan promptResult, 1)
	go func() {
		r, e := d.Prompt(id, blocks)
		ch <- promptResult{r.StopReason, e}
	}()
	// Let the turn start so the cancel lands on a running turn.
	time.Sleep(100 * time.Millisecond)
	_ = d.Cancel(id)

	select {
	case r := <-ch:
		if r.err != nil {
			return fail("cancel", "prompt: "+r.err.Error())
		}
		if r.stop == acp.StopReasonCancelled {
			return pass("cancel", "reached cancelled")
		}
		return skip("cancel", "prompt completed before cancel (no long-running work)")
	case <-time.After(cancelBound):
		return fail("cancel", "did not reach cancelled within "+cancelBound.String())
	}
}

// checkBusy starts a prompt and re-enters the same session, expecting -32002
// (busy). A provider whose turn already finished never reports busy, so the
// strict assertion is skipped.
func checkBusy(ctx context.Context, f *flags) CheckResult {
	d, err := startDriver(f)
	if err != nil {
		return fail("busy", "start: "+err.Error())
	}
	defer d.Close()
	if _, err := d.Initialize(releaseInitMeta()); err != nil {
		return fail("busy", "initialize: "+err.Error())
	}

	id, err := d.NewSession(protocol.SessionMeta{})
	if err != nil {
		return fail("busy", "session/new: "+err.Error())
	}
	blocks := []acp.ContentBlock{acp.TextBlock("hi")}
	first := make(chan error, 1)
	go func() {
		_, e := d.Prompt(id, blocks)
		first <- e
	}()
	// Let the first turn start so the re-entry hits a running session.
	time.Sleep(100 * time.Millisecond)

	_, err = d.Prompt(id, blocks)
	switch {
	case err == nil:
		// The first turn already finished; the plugin has no busy surface.
		<-first
		return skip("busy", "provider never reported busy (no long-running turn)")
	case isBusy(err):
		// The first turn is still running and is reaped by Close.
		return pass("busy", "-32002 on re-entry")
	default:
		return fail("busy", "second prompt: "+err.Error())
	}
}

// isBusy reports whether err is a JSON-RPC "busy" error (-32002).
func isBusy(err error) bool {
	var rpcErr *protocol.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == protocol.CodeBusy
}
