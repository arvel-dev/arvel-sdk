// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"github.com/arvel-dev/arvel-sdk/protocol"
	"github.com/arvel-dev/arvel-sdk/testkit/mockhost"
	acp "github.com/coder/acp-go-sdk"
	"sync"
	"time"
)

const cancelBound = 15 * time.Second

// checkConcurrency keeps two requests outstanding and exercises advertised load.
func checkConcurrency(ctx context.Context, f *flags) (result CheckResult) {
	r, err := prepareCheck(ctx, f, "concurrency", nil, resolveFakevendorDir(f.scenarios), "")
	if err != nil {
		return fail("concurrency", safeError(err))
	}
	defer r.finishOne(&result)
	init, err := r.initialize()
	if err != nil {
		return fail("concurrency", "initialize: "+safeError(err))
	}
	if err = r.ready(); err != nil {
		return fail("concurrency", safeError(err))
	}
	a, err := r.newSession()
	if err != nil {
		return fail("concurrency", "session/new 1: "+safeError(err))
	}
	b, err := r.newSession()
	if err != nil {
		return fail("concurrency", "session/new 2: "+safeError(err))
	}
	if a == "" || b == "" || a == b {
		return fail("concurrency", "session/new must return two distinct session IDs")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []string{a, b} {
		wg.Go(func() { _, e := r.promptSession(id); errs <- e })
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			return fail("concurrency", "prompt: "+safeError(e))
		}
	}
	if _, err = r.promptSession(a); err != nil {
		return fail("concurrency", "second turn: "+safeError(err))
	}
	if init.Has(protocol.FeatureResume) {
		err = r.call(protocol.MethodSessionLoad, map[string]any{"sessionId": b, "cwd": r.prepared.Environment().Workspace(), "mcpServers": []any{}, "_meta": protocol.WithMeta(nil, protocol.SessionMeta{})}, nil)
		if err != nil {
			return fail("concurrency", "announced resume: "+safeError(err))
		}
	}
	return pass("concurrency", "2 sessions interleaved ok")
}

// waitActive waits for streaming evidence in configured probes. Legacy probes
// retain their short grace and documented fast-turn SKIP behavior.
func (r *checkRun) waitActive(id string, done <-chan struct{}) error {
	if !r.strict {
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-r.ctx.Done():
			return r.ctx.Err()
		}
	}
	timer := time.NewTimer(initTimeout)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return errors.New("checks.prompt: turn finished before active assertion")
		default:
		}
		if len(r.d.Updates(id)) > 0 {
			return nil
		}
		select {
		case <-done:
			return errors.New("checks.prompt: turn finished before active assertion")
		case <-r.ctx.Done():
			return r.ctx.Err()
		case <-timer.C:
			return errors.New("checks.prompt: no active streaming turn observed")
		case <-tick.C:
		}
	}
}

type turnOutcome struct {
	result mockhost.PromptResult
	err    error
}

// checkCancel requires an actual cancelled turn; configured early completion fails.
func checkCancel(ctx context.Context, f *flags) (result CheckResult) {
	r, err := prepareCheck(ctx, f, "cancel", nil, resolveFakevendorDir(f.scenarios), "")
	if err != nil {
		return fail("cancel", safeError(err))
	}
	var wg sync.WaitGroup
	defer func() { _ = r.d.CloseWithError(); wg.Wait(); r.finishOne(&result) }()
	if _, err = r.initialize(); err != nil {
		return fail("cancel", "initialize: "+safeError(err))
	}
	if err = r.ready(); err != nil {
		return fail("cancel", safeError(err))
	}
	id, err := r.newSession()
	if err != nil {
		return fail("cancel", "session/new: "+safeError(err))
	}
	ch := make(chan turnOutcome, 1)
	done := make(chan struct{})
	wg.Go(func() { p, e := r.promptSession(id); ch <- turnOutcome{p, e}; close(done) })
	if err = r.waitActive(id, done); err != nil {
		return fail("cancel", safeError(err))
	}
	if err = r.d.Cancel(id); err != nil {
		return fail("cancel", "cancel notification: "+safeError(err))
	}
	timer := time.NewTimer(cancelBound)
	defer timer.Stop()
	select {
	case out := <-ch:
		if out.err != nil {
			return fail("cancel", "prompt: "+safeError(out.err))
		}
		if out.result.StopReason == acp.StopReasonCancelled {
			return pass("cancel", "reached cancelled")
		}
		if r.strict {
			return fail("cancel", "checks.prompt: configured turn did not reach cancelled")
		}
		return skip("cancel", "prompt completed before cancel (no long-running work)")
	case <-ctx.Done():
		return fail("cancel", safeError(ctx.Err()))
	case <-timer.C:
		return fail("cancel", "did not reach cancelled within "+cancelBound.String())
	}
}

// checkBusy re-enters an active session and always joins its first prompt.
func checkBusy(ctx context.Context, f *flags) (result CheckResult) {
	r, err := prepareCheck(ctx, f, "busy", nil, resolveFakevendorDir(f.scenarios), "")
	if err != nil {
		return fail("busy", safeError(err))
	}
	var wg sync.WaitGroup
	defer func() { _ = r.d.CloseWithError(); wg.Wait(); r.finishOne(&result) }()
	if _, err = r.initialize(); err != nil {
		return fail("busy", "initialize: "+safeError(err))
	}
	if err = r.ready(); err != nil {
		return fail("busy", safeError(err))
	}
	id, err := r.newSession()
	if err != nil {
		return fail("busy", "session/new: "+safeError(err))
	}
	done := make(chan struct{})
	wg.Go(func() { _, _ = r.promptSession(id); close(done) })
	if err = r.waitActive(id, done); err != nil {
		return fail("busy", safeError(err))
	}
	_, err = r.promptSession(id)
	if isBusy(err) {
		return pass("busy", "-32002 on re-entry")
	}
	if err != nil {
		return fail("busy", "second prompt: "+safeError(err))
	}
	if r.strict {
		return fail("busy", "checks.prompt: configured turn did not report busy")
	}
	return skip("busy", "provider never reported busy (no long-running turn)")
}
func isBusy(err error) bool {
	var rpcErr *protocol.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == protocol.CodeBusy
}
