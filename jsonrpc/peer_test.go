// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arvel-dev/arvel-sdk/internal/leakcheck"
	"github.com/arvel-dev/arvel-sdk/protocol"
)

// syncBuf is a goroutine-safe bytes.Buffer.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newLogger(buf *syncBuf) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// recorder copies everything written through it into buf.
type recorder struct {
	w   io.Writer
	buf syncBuf
}

func (r *recorder) Write(p []byte) (int, error) {
	r.buf.Write(p)
	return r.w.Write(p)
}

type pair struct {
	client, server *Peer
	toServer       *recorder // bytes the client wrote
	logs           *syncBuf  // server log
}

// newPair connects a client and a server peer through two pipes and serves
// both. Cleanup closes the pipes and checks for leaked goroutines.
func newPair(t *testing.T, serverOpts ...Option) *pair {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	rec := &recorder{w: c2sW}
	logs := &syncBuf{}
	server := NewPeer(c2sR, s2cW, append([]Option{WithLogger(newLogger(logs))}, serverOpts...)...)
	client := NewPeer(s2cR, rec)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for _, p := range []*Peer{server, client} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Serve(ctx)
		}()
	}
	t.Cleanup(func() {
		_ = c2sW.Close()
		_ = s2cW.Close()
		wg.Wait()
		cancel()
		leakcheck.Check(t, "jsonrpc.(*Peer)")
	})
	return &pair{client: client, server: server, toServer: rec, logs: logs}
}

func TestPeer_RoundTrip(t *testing.T) {
	p := newPair(t)
	p.server.Handle("echo", func(_ context.Context, params json.RawMessage) (any, error) {
		return json.RawMessage(params), nil
	})
	var got map[string]int
	if err := p.client.Call(context.Background(), "echo", map[string]int{"a": 1}, &got); err != nil {
		t.Fatal(err)
	}
	if got["a"] != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestPeer_UnknownMethodIsMethodNotFound(t *testing.T) {
	p := newPair(t)
	err := p.client.Call(context.Background(), "_arvel/nope", nil, nil)
	var re *protocol.RPCError
	if !errors.As(err, &re) || re.Code != protocol.CodeMethodNotFound {
		t.Fatalf("err = %v, want -32601", err)
	}
}

func TestPeer_HandlerRPCErrorPassesThrough(t *testing.T) {
	p := newPair(t)
	p.server.Handle("m", func(context.Context, json.RawMessage) (any, error) {
		return nil, protocol.NewError(protocol.ErrBusy, "busy now", nil)
	})
	err := p.client.Call(context.Background(), "m", nil, nil)
	var re *protocol.RPCError
	if !errors.As(err, &re) || re.Code != protocol.CodeBusy {
		t.Fatalf("err = %v, want -32002", err)
	}
	if d, ok := re.Arvel(); !ok || d.Code != protocol.ErrBusy {
		t.Fatalf("arvel data = %+v, %v", d, ok)
	}
}

func TestPeer_PlainErrorIsGenericInternal(t *testing.T) {
	p := newPair(t)
	p.server.Handle("m", func(context.Context, json.RawMessage) (any, error) {
		return nil, errors.New("secret sk-ant-xxxxxxxxxxxx")
	})
	err := p.client.Call(context.Background(), "m", nil, nil)
	var re *protocol.RPCError
	if !errors.As(err, &re) || re.Message != "internal error" || strings.Contains(err.Error(), "sk-ant") {
		t.Fatalf("err = %v", err)
	}
}

func TestPeer_PanicBecomesInternalError(t *testing.T) {
	p := newPair(t)
	p.server.Handle("boom", func(context.Context, json.RawMessage) (any, error) { panic("kaboom") })
	p.server.Handle("ok", func(context.Context, json.RawMessage) (any, error) { return "fine", nil })

	err := p.client.Call(context.Background(), "boom", nil, nil)
	var re *protocol.RPCError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v", err)
	}
	if d, ok := re.Arvel(); !ok || d.Code != protocol.ErrInternal {
		t.Fatalf("arvel data = %+v, %v", d, ok)
	}
	var s string
	if err := p.client.Call(context.Background(), "ok", nil, &s); err != nil || s != "fine" {
		t.Fatalf("server must keep serving: %v %q", err, s)
	}
	logs := p.logs.String()
	if !strings.Contains(logs, `"level":"ERROR"`) || !strings.Contains(logs, `"stack"`) {
		t.Fatalf("panic not logged with a stack: %s", logs)
	}
}

func TestPeer_CallCancelSendsNothing(t *testing.T) {
	p := newPair(t)
	started := make(chan struct{})
	release := make(chan struct{})
	p.server.Handle("slow", func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- p.client.Call(ctx, "slow", nil, nil) }()
	<-started
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	lines := strings.Split(strings.TrimSpace(p.toServer.buf.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"method":"slow"`) {
		t.Fatalf("client wrote %d lines: %q", len(lines), lines)
	}
}

func TestPeer_ConcurrentRequests(t *testing.T) {
	const n = 50
	p := newPair(t)
	var arrived sync.WaitGroup
	arrived.Add(n)
	p.server.Handle("barrier", func(context.Context, json.RawMessage) (any, error) {
		arrived.Done()
		arrived.Wait() // returns only when all n handlers run at once
		return nil, nil
	})
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.client.Call(context.Background(), "barrier", nil, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestPeer_ServeEndsOnEOFAndFailsPending(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	peer := NewPeer(r1, w2)
	served := make(chan error, 1)
	go func() { served <- peer.Serve(context.Background()) }()
	go func() { _, _ = io.Copy(io.Discard, r2) }() // drain the request
	errc := make(chan error, 1)
	go func() { errc <- peer.Call(context.Background(), "never", nil, nil) }()
	time.Sleep(50 * time.Millisecond)
	_ = w1.Close()
	if err := <-errc; !errors.Is(err, ErrClosed) {
		t.Fatalf("pending call err = %v, want ErrClosed", err)
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve = %v, want nil", err)
	}
	_ = w2.Close()
	leakcheck.Check(t, "jsonrpc.(*Peer)")
}

func TestPeer_ServeContextCancelReleasesHandlers(t *testing.T) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	peer := NewPeer(r1, w2)
	entered := make(chan struct{})
	exited := make(chan struct{})
	peer.Handle("wait", func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(entered)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- peer.Serve(ctx) }()
	go func() { _, _ = io.Copy(io.Discard, r2) }()
	if _, err := io.WriteString(w1, `{"jsonrpc":"2.0","id":1,"method":"wait"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	<-entered
	cancel()
	if err := <-served; !errors.Is(err, context.Canceled) {
		t.Fatalf("Serve = %v, want context.Canceled", err)
	}
	<-exited
	_ = w1.Close()
	_ = w2.Close()
	leakcheck.Check(t, "jsonrpc.(*Peer)")
}

func TestPeer_DrainTimeout(t *testing.T) {
	logs := &syncBuf{}
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	peer := NewPeer(r1, w2, WithLogger(newLogger(logs)), WithDrainTimeout(50*time.Millisecond))
	entered := make(chan struct{})
	release := make(chan struct{})
	peer.Handle("stuck", func(context.Context, json.RawMessage) (any, error) {
		close(entered)
		<-release // ignores its context
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- peer.Serve(ctx) }()
	go func() { _, _ = io.Copy(io.Discard, r2) }()
	_, _ = io.WriteString(w1, `{"jsonrpc":"2.0","id":1,"method":"stuck"}`+"\n")
	<-entered
	cancel()
	select {
	case <-served:
	case <-time.After(time.Second):
		t.Fatal("Serve did not return within the drain timeout")
	}
	if !strings.Contains(logs.String(), "handlers still running after drain") {
		t.Fatalf("missing drain warning: %s", logs.String())
	}
	close(release)
	_ = w1.Close()
	_ = w2.Close()
	leakcheck.Check(t, "jsonrpc.(*Peer)")
}

func TestPeer_ResponseTooLargeBecomesInternalError(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates 65 MiB")
	}
	p := newPair(t)
	p.server.Handle("big", func(context.Context, json.RawMessage) (any, error) {
		return strings.Repeat("a", 65<<20), nil
	})
	err := p.client.Call(context.Background(), "big", nil, nil)
	var re *protocol.RPCError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v", err)
	}
	if d, ok := re.Arvel(); !ok || d.Code != protocol.ErrInternal {
		t.Fatalf("arvel data = %+v, %v", d, ok)
	}
}

func TestPeer_ServeTwice(t *testing.T) {
	r, w := io.Pipe()
	peer := NewPeer(r, io.Discard)
	done := make(chan struct{})
	go func() { _ = peer.Serve(context.Background()); close(done) }()
	for {
		peer.mu.Lock()
		serving := peer.serving
		peer.mu.Unlock()
		if serving {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := peer.Serve(context.Background()); !errors.Is(err, ErrServing) {
		t.Fatalf("Serve = %v, want ErrServing", err)
	}
	_ = w.Close()
	<-done
}

func TestPeer_NotifyAfterCloseIsErrClosed(t *testing.T) {
	r1, w1 := io.Pipe()
	peer := NewPeer(r1, io.Discard)
	done := make(chan struct{})
	go func() { _ = peer.Serve(context.Background()); close(done) }()
	_ = w1.Close()
	<-done
	if err := peer.Notify("x", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Notify = %v", err)
	}
	if err := peer.Call(context.Background(), "x", nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Call = %v", err)
	}
}

// rawConn talks to a server peer line by line.
type rawConn struct {
	w  io.Writer
	sc *bufio.Scanner
}

func (c *rawConn) send(t *testing.T, line string) {
	t.Helper()
	if _, err := io.WriteString(c.w, line+"\n"); err != nil {
		t.Fatal(err)
	}
}

func (c *rawConn) recv(t *testing.T) map[string]any {
	t.Helper()
	type res struct {
		line string
		ok   bool
	}
	ch := make(chan res, 1)
	go func() { ok := c.sc.Scan(); ch <- res{c.sc.Text(), ok} }()
	select {
	case r := <-ch:
		if !r.ok {
			t.Fatal("connection closed")
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(r.line), &m); err != nil {
			t.Fatalf("not JSON: %q", r.line)
		}
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no line within 2s")
		return nil
	}
}

func newRaw(t *testing.T, opts ...Option) (*Peer, *rawConn) {
	t.Helper()
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	peer := NewPeer(r1, w2, opts...)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = peer.Serve(ctx); close(done) }()
	sc := bufio.NewScanner(r2)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	t.Cleanup(func() {
		_ = w1.Close()
		<-done
		cancel()
		_ = w2.Close()
		leakcheck.Check(t, "jsonrpc.(*Peer)")
	})
	return peer, &rawConn{w: w1, sc: sc}
}

func TestPeer_StringIDEchoed(t *testing.T) {
	peer, c := newRaw(t)
	peer.Handle("m", func(context.Context, json.RawMessage) (any, error) { return 1, nil })
	c.send(t, `{"jsonrpc":"2.0","id":"abc","method":"m"}`)
	if got := c.recv(t)["id"]; got != "abc" {
		t.Fatalf("id = %v", got)
	}
}

func TestPeer_InvalidLinesGetErrorReplies(t *testing.T) {
	peer, c := newRaw(t)
	peer.Handle("m", func(context.Context, json.RawMessage) (any, error) { return 1, nil })
	for _, tc := range []struct {
		line string
		code float64
	}{
		{`{"jsonrpc":`, protocol.CodeParseError},
		{`[1,2]`, protocol.CodeInvalidRequest},
	} {
		c.send(t, tc.line)
		m := c.recv(t)
		e, _ := m["error"].(map[string]any)
		if m["id"] != nil || e == nil || e["code"] != tc.code {
			t.Fatalf("reply to %q = %v", tc.line, m)
		}
	}
	c.send(t, `{"jsonrpc":"2.0","id":5,"method":"m"}`)
	if m := c.recv(t); m["result"] != float64(1) {
		t.Fatalf("connection must continue: %v", m)
	}
}

func TestPeer_OverLongLineIsDroppedAndConnectionSurvives(t *testing.T) {
	peer, c := newRaw(t, WithMaxLine(1024))
	peer.Handle("m", func(context.Context, json.RawMessage) (any, error) { return "ok", nil })
	c.send(t, `{"jsonrpc":"2.0","id":1,"method":"m","params":"`+strings.Repeat("x", 4096)+`"}`)
	m := c.recv(t)
	e, _ := m["error"].(map[string]any)
	if m["id"] != nil || e == nil || e["code"] != float64(protocol.CodeInvalidRequest) {
		t.Fatalf("reply = %v", m)
	}
	c.send(t, `{"jsonrpc":"2.0","id":2,"method":"m"}`)
	if m := c.recv(t); m["result"] != "ok" {
		t.Fatalf("connection must continue: %v", m)
	}
}

func TestPeer_UnhandledNotificationIgnored(t *testing.T) {
	peer, c := newRaw(t)
	peer.Handle("m", func(context.Context, json.RawMessage) (any, error) { return "ok", nil })
	c.send(t, `{"jsonrpc":"2.0","method":"nobody/listens"}`)
	c.send(t, `{"jsonrpc":"2.0","id":1,"method":"m"}`)
	if m := c.recv(t); m["id"] != float64(1) {
		t.Fatalf("first reply must belong to the request: %v", m)
	}
}

func TestPeer_NotificationsInOrderBeforeResponse(t *testing.T) {
	p := newPair(t)
	p.server.Handle("work", func(context.Context, json.RawMessage) (any, error) {
		for i := range 50 {
			if err := p.server.Notify("note", map[string]any{"sessionId": "s1", "n": i}); err != nil {
				return nil, err
			}
		}
		return "done", nil
	})
	var mu sync.Mutex
	var seen []int
	p.client.HandleNotification("note", func(_ context.Context, params json.RawMessage) {
		var v struct{ N int }
		_ = json.Unmarshal(params, &v)
		time.Sleep(time.Millisecond) // give a wrong implementation the chance to reorder
		mu.Lock()
		seen = append(seen, v.N)
		mu.Unlock()
	})
	if err := p.client.Call(context.Background(), "work", nil, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 50 {
		t.Fatalf("Call returned after %d of 50 notifications", len(seen))
	}
	for i, n := range seen {
		if n != i {
			t.Fatalf("notification %d handled out of order: %v", i, seen)
		}
	}
}

func TestPeer_DifferentSessionsRunInParallel(t *testing.T) {
	p := newPair(t)
	p.server.Handle("go", func(context.Context, json.RawMessage) (any, error) {
		_ = p.server.Notify("note", map[string]any{"sessionId": "a"})
		_ = p.server.Notify("note", map[string]any{"sessionId": "b"})
		return nil, nil
	})
	bRan := make(chan struct{})
	p.client.HandleNotification("note", func(ctx context.Context, params json.RawMessage) {
		var v struct{ SessionID string }
		_ = json.Unmarshal(params, &v)
		if v.SessionID == "b" {
			close(bRan)
			return
		}
		select { // session a waits for session b
		case <-bRan:
		case <-time.After(2 * time.Second):
			t.Error("session b was blocked behind session a")
		}
	})
	if err := p.client.Call(context.Background(), "go", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPeer_CallFromNotificationHandlerDoesNotDeadlock(t *testing.T) {
	p := newPair(t)
	p.server.Handle("echo", func(context.Context, json.RawMessage) (any, error) { return "pong", nil })
	got := make(chan string, 1)
	p.client.HandleNotification("ping", func(ctx context.Context, _ json.RawMessage) {
		var s string
		if err := p.client.Call(ctx, "echo", nil, &s); err != nil {
			s = fmt.Sprint("error: ", err)
		}
		got <- s
	})
	if err := p.server.Notify("ping", map[string]any{"sessionId": "s"}); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if s != "pong" {
			t.Fatalf("got %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock")
	}
}

func TestPeer_PanicInNotificationHandler(t *testing.T) {
	p := newPair(t)
	var n int
	var mu sync.Mutex
	handled := make(chan struct{}, 1)
	p.client.HandleNotification("note", func(context.Context, json.RawMessage) {
		mu.Lock()
		n++
		first := n == 1
		mu.Unlock()
		if first {
			panic("first notification fails")
		}
		handled <- struct{}{}
	})
	_ = p.server.Notify("note", map[string]any{"sessionId": "s"})
	_ = p.server.Notify("note", map[string]any{"sessionId": "s"})
	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("second notification was not handled after a panic")
	}
}
