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

### Changed
- `docs/PROTOCOL.md` synced with the workspace sandbox (S7): new §15 Workspace
  sandbox, the manifest `sandbox` block, the `initialize` `sandbox` field, one
  provider process per connection and workspace, provider security rule 6 and the
  conformance note. The change is additive within `pluginApi: "1"`.

### API changes
- None. The module exports no identifiers yet.
