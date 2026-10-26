# Spike S0.1: ACP SDK capabilities

A committed spike for SPEC section 5.3. It checks whether `github.com/coder/acp-go-sdk`
can serve as the connection layer of the Arvel SDK. It is a separate Go module so the
root `go.mod` stays free of dependencies.

| Probe | SPEC point | Test file |
|---|---|---|
| 1 | custom `_` methods received and answered by the agent | `probe1_extension_methods_test.go` |
| 2 | custom notifications sent by the agent | `probe1_extension_methods_test.go` |
| 3 | `_meta` on initialize, session/new, prompt result, session/update, request_permission | `probe3_meta_test.go` |
| 4 | lines up to 64 MiB | `probe4_line_size_test.go` |
| 5 | permission request answered after an unbounded wait while other sessions continue | `probe5_permission_test.go` |
| 6 | client side usable (mock host, Arvel host) | `probe6_client_side_test.go` |

Run from this directory: `go test -count=1 -v ./...`, and filter the verdicts with
`grep -E "PROBE|OBSERVATION"` on its output. `-short` skips probe 4 and shortens probe 5.

Results and decision: [../../docs/FINDINGS.md](../../docs/FINDINGS.md)
