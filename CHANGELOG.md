# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows
[Semantic Versioning](https://semver.org/). Before v1.0.0 a minor release may
change the Go API; every such change carries a migration note. The protocol
(`pluginApi: "1"`) never breaks.

## [Unreleased]

### Added
- Go module `github.com/arvel-dev/arvel-sdk` with a documented root package.
- `docs/PROTOCOL.md`: the Arvel Provider Protocol v1, now the source of truth.
- CI on Linux, macOS and Windows (`go vet`, `staticcheck`, `go test -race`) and a
  check that every `.go` file carries an SPDX licence header.
- `docs/FINDINGS.md`: results of spike S0.1 (ACP SDK capabilities). Decision: the
  SDK ships its own `jsonrpc/` package, because `acp-go-sdk` limits lines to 10 MiB
  and the protocol requires 64 MiB; only its schema types are used.

- `protocol` package: wire types for every `_meta.arvel` and `_arvel/*` structure,
  enums with `Valid()`, the error model (`ErrorCode`, `RPCError`, `NewError`),
  `MetaFrom` and `WithMeta`, and the router types with `Validate` on every router
  result.
- `manifest` package: `Load`, `LoadFile`, `Validate` (JSON Schema plus semantic
  checks, including the `sandbox` block and its forbidden-path rules), `EntryFor`
  and `DiffPermissions` for update permission diffs.
- `schema/arvel.schema.json` and the `schema` package that embeds it.
- Golden tests that bind every example of `docs/PROTOCOL.md` to the Go types; a
  new example in the document fails the build until it has a golden file.

- `jsonl` package: a line scanner that accepts lines up to 64 MiB and keeps
  working after an over-long line, and a concurrency-safe JSON line writer.
- `redact` package: secret scrubbing for logs and errors (registered secrets plus
  common key formats).
- `logx` package: the protocol's JSON stderr logger with the level from
  `ARVEL_LOG_LEVEL`, redaction of every string, and `Truncate` for debug prompts.
- `procx` package: finding vendor binaries, version probe, a minimal child
  environment (with the sandbox proxy variables and a list of variables that are
  never passed on), and starting, interrupting and killing whole process trees on
  Linux, macOS and Windows, including `.cmd` and `.bat` shims.

### Changed
- `docs/PROTOCOL.md` synced with the workspace sandbox (S7): new §15 Workspace
  sandbox, the manifest `sandbox` block, the `initialize` `sandbox` field, one
  provider process per connection and workspace, provider security rule 6 and the
  conformance note. The change is additive within `pluginApi: "1"`.

### API changes
- New exported packages `protocol`, `manifest`, `schema`, `jsonl`, `redact`,
  `logx` and `procx`. All changes are additive; there are no breaking changes.
- New dependencies: `github.com/santhosh-tekuri/jsonschema/v6` and
  `golang.org/x/mod` (plus `golang.org/x/text`, which `jsonschema` needs), and
  `golang.org/x/sys` (used only by the Windows code of `procx`).
