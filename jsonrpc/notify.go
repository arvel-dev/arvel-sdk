// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync"
)

// item is one queued or running notification.
type item struct {
	method string
	params json.RawMessage
	h      NotificationFunc
	done   chan struct{}
}

// notifier keeps one FIFO queue, with one worker goroutine, per key. A key is
// present in queues exactly while its worker runs.
type notifier struct {
	mu     sync.Mutex
	queues map[string][]*item
	live   map[*item]struct{} // queued or running
	wg     sync.WaitGroup     // workers
}

func (n *notifier) workers() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.queues)
}

// snapshot returns the done channels of all notifications received so far that
// have not finished.
func (n *notifier) snapshot() []chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.live) == 0 {
		return nil
	}
	out := make([]chan struct{}, 0, len(n.live))
	for it := range n.live {
		out = append(out, it.done)
	}
	return out
}

func (p *Peer) dispatchNotification(m message) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	h := p.notifs[m.Method]
	ctx := p.baseCtx
	if h == nil {
		p.mu.Unlock()
		p.cfg.log.Debug("unhandled notification", "method", m.Method)
		return
	}
	key := p.cfg.keyFn(m.Method, m.Params)
	it := &item{method: m.Method, params: m.Params, h: h, done: make(chan struct{})}

	n := &p.nt
	n.mu.Lock()
	n.live[it] = struct{}{}
	q, active := n.queues[key]
	n.queues[key] = append(q, it)
	if !active {
		n.wg.Add(1)
	}
	n.mu.Unlock()
	p.mu.Unlock()

	if !active {
		go p.worker(ctx, key)
	}
}

func (p *Peer) worker(ctx context.Context, key string) {
	n := &p.nt
	defer n.wg.Done()
	ctx = context.WithValue(ctx, inNotification{}, true)
	for {
		n.mu.Lock()
		q := n.queues[key]
		if len(q) == 0 {
			delete(n.queues, key)
			n.mu.Unlock()
			return
		}
		it := q[0]
		n.queues[key] = q[1:]
		n.mu.Unlock()

		p.runNotification(ctx, it)

		n.mu.Lock()
		delete(n.live, it)
		n.mu.Unlock()
		close(it.done)
	}
}

func (p *Peer) runNotification(ctx context.Context, it *item) {
	defer func() {
		if r := recover(); r != nil {
			p.cfg.log.Error("notification handler panic", "method", it.method,
				"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	it.h(ctx, it.params)
}
