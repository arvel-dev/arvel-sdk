// SPDX-License-Identifier: Apache-2.0

// Package jsonrpc is the bidirectional, line-delimited JSON-RPC 2.0 peer of the
// Arvel Provider Protocol (PROTOCOL §2). It sits on jsonl.Scanner and
// jsonl.Writer and replaces the connection layer of the ACP library, which
// limits lines to 10 MiB (docs/FINDINGS.md, spike S0.1).
//
// # Concurrency and ordering
//
// Every incoming request runs in its own goroutine. Notifications that carry
// the same key (by default their params.sessionId) are handled one at a time in
// arrival order; notifications with different keys run in parallel. A response
// is handed to its Call only after every notification received before it has
// been handled, so a client sees all session/update messages of a turn before
// the session/prompt result. On the sending side Notify returns after the line
// has been written, so Notify calls followed by a handler's return reach the
// wire in that order.
//
// A notification handler may call Call, but the response is then not ordered
// after other notifications (that would deadlock). A notification handler must
// never wait for a request handler of the same peer.
//
// # Cancellation
//
// Cancelling the context of Call writes nothing: ACP cancellation is semantic,
// through session/cancel. A late response to a cancelled call is dropped.
//
// # Errors
//
// Errors on the wire are *protocol.RPCError. A handler that returns another
// error produces the generic "internal error" without the error text, since the
// text may hold a secret. A handler panic is recovered and answered with the
// Arvel error "internal".
//
// # Shutdown
//
// Serve returns when the reader reaches end of input, fails, or the context
// ends. A goroutine blocked in Read cannot be interrupted; it ends when the
// caller closes the reader. Serve does not wait for it.
package jsonrpc
