# Findings

Results of VERIFY spikes. Each spike has its own section.

## S0.1 ACP SDK capabilities

- Date: 2026-10-26
- SDK: `github.com/coder/acp-go-sdk` v0.13.5 (newest tag on the Go module proxy that day)
- Go: go1.27.2 darwin/arm64
- OS: Darwin arm64
- Reproduce: change into `spikes/s0-1-acp`, then run `go test -count=1 -v ./...` as a separate command and filter the output for `PROBE` and `OBSERVATION`.

The questions are the six points of SPEC section 5.3. The probes run a real agent connection and a real client connection of the library against each other over in-memory pipes.

| # | Question (SPEC §5.3) | Result | Evidence |
|---|---|---|---|
| 1 | The agent receives and answers custom methods with a `_` prefix (`_arvel/status`); unknown ones return `-32601`. | YES | `TestProbe1_AgentReceivesCustomMethod`: `PROBE 1 YES: custom method answered=yes, unknown _arvel/* code -32601=yes` |
| 2 | The agent can send custom notifications (`_arvel/usage_updated`). | YES | `TestProbe2_AgentSendsCustomNotification`: `PROBE 2 YES: client received method=_arvel/usage_updated` |
| 3 | `_meta` can be read and written on `initialize`, `session/new`, the `session/prompt` result, `session/update` and `session/request_permission`. | YES | `TestProbe3_MetaRoundTrip` (5 subtests): `PROBE 3 YES: all five _meta locations preserved` |
| 4 | The connection accepts lines up to 64 MiB. | **NO** | `TestProbe4_LargeLines`: `PROBE 4 NO: largest working line: client->agent=9 MiB, agent->client=9 MiB (required 64 MiB)` |
| 5 | The agent can send `session/request_permission` and wait an unbounded time for the answer while other sessions continue. | YES | `TestProbe5_UnboundedPermissionWaitsWhileOthersContinue`: `PROBE 5 YES: pending for 15s without timeout=yes, other session unblocked=yes` |
| 6 | The library can be used from the client side too (for `testkit/mockhost` and the Arvel host). | YES | `TestProbe6_ClientSideDrivesAgent`: `PROBE 6 YES: 7 steps ok of 7, public API only` |

### Additional observations

- **Line limit (point 4).** `connection.go`, function `receive`, creates a `bufio.Scanner` with `maxBufSize = 10 * 1024 * 1024`. The probe confirms it: 9 MiB works in both directions, 11 MiB, 32 MiB and 64 MiB all fail. The limit is a constant, not an option. When a line is too long the reader stops with `bufio.Scanner: token too long` and closes the connection: the pending call gets `peer disconnected before response` or blocks, and a follow-up call on the same connection fails. PROTOCOL §2 requires both sides to accept 64 MiB, and the `jsonl.Scanner` of SPEC §5.6 must survive a too-long line and keep working, so the library cannot meet either requirement.
- **Cancel on context (not a SPEC point).** When the context of a pending `Prompt` call is cancelled, the library writes two messages to the wire: `{"method":"session/cancel","params":{"sessionId":"s_1"}}` and `{"method":"$/cancel_request","params":{"requestId":1}}` (`TestObservation_ContextCancelSendsCancelMessage`). SPEC §5.3 says that cancelling the context of `Call` sends nothing, because ACP cancellation is semantic via `session/cancel`. With the library, the host side would send `$/cancel_request` as well, and the plugin side would have to tolerate it.
- **Cancel of an agent request.** Cancelling the context of the agent's pending `RequestPermission` returns an error (`code -32800`, "Request cancelled") immediately, and the client handler's context is cancelled too (`TestProbe5_CancelEndsPendingPermission`).
- **No built-in request timeout.** The only `WithTimeout` in the library sources is in `waitForNotificationDrain` (`connection.go` line 837), used on shutdown. There is no timeout on requests.
- **Ordering.** Fifty numbered `session/update` notifications sent before the prompt result all arrived, in order, before the client's `Prompt` call returned (probe 6).
- **Error codes.** A handler error is converted to a JSON-RPC error by `toReqErr`: only a returned `*acp.RequestError` keeps its code (for example `acp.NewMethodNotFound` gives `-32601`); any other error becomes `-32603` "Internal error".
- **Extension API.** Both sides implement `acp.ExtensionMethodHandler`, and both connection types have `CallExtension` and `NotifyExtension` for method names starting with `_`.
- **Schema types.** `Meta map[string]any` with the tag `_meta` exists on all request, response and update types the probes used.

### Decision

**Decision: implement jsonrpc/ (SPEC §5.3) and use only the schema types from the SDK.**

- `jsonrpc/` is created in S3 as described in SPEC §5.3, with `jsonl.Scanner` (S2, 64 MiB, survives a too-long line) as the reader and the fuzz test `FuzzPeerDecode`. The library's connection layer (`acp.NewConnection`, `AgentSideConnection`, `ClientSideConnection`) is not used.
- S1 `protocol` and S3 `plugin` import `github.com/coder/acp-go-sdk` for the ACP schema types only (`acp.ContentBlock`, `acp.SessionUpdate`, `acp.InitializeRequest` and similar). `Validate()` methods and JSON marshalling of those types are reused. The library is not forked.
- The own `jsonrpc.Peer` must meet the requirements the library fails or differs on: lines up to 64 MiB; a too-long line is discarded with `ErrLineTooLong` and the connection keeps working; cancelling a `Call` context sends nothing on the wire; unknown `_arvel/*` methods return `-32601`.
- `testkit/mockhost` and the Arvel host use the same `jsonrpc.Peer` on the client side. The library's client side works (probe 6) but would inherit the 10 MiB limit, so it is not used there.
- The ordering guarantee seen in probe 6 (all notifications of a session delivered before the response of the prompt) is a property the own peer must keep, and S3 tests it.

### SPEC impact

None. Both outcomes are covered by the decision rule in SPEC §5.3; creating `jsonrpc/` is the rule's result for a NO on point 4, not a contradiction of the SPEC.

## Notes

- `docs/PROTOCOL.md` is the source of truth from S0 on. The copy in `.ai-factory/brief/` is a stale snapshot and must not be edited.
