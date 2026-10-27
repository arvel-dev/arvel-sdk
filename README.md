# Arvel SDK

[![CI](https://github.com/arvel-dev/arvel-sdk/actions/workflows/ci.yml/badge.svg)](https://github.com/arvel-dev/arvel-sdk/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/arvel-dev/arvel-sdk.svg)](https://pkg.go.dev/github.com/arvel-dev/arvel-sdk)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

The Go SDK for writing **plugins for [Arvel](https://github.com/arvel-dev/arvel)**, and the conformance tests every plugin must pass. Four kinds are supported:

- **provider plugins** connect Arvel to an AI coding agent;
- **router plugins** suggest a task's type and which roles it needs (for example [arvel-plugin-jev](https://github.com/arvel-dev/arvel-plugin-jev)). Without one, the task type the user picks decides the roles;
- **team plugins** add roles and task types — no code, only `arvel.json` and instruction files;
- **integration plugins** add MCP servers and skills for agents, and may add roles and task types too.

A provider plugin connects Arvel to an AI coding agent (Claude, ChatGPT/Codex, OpenCode, …). Arvel assigns that agent a role, such as Architect, Developer or Reviewer, and gives it a model and reasoning effort. Arvel also stays in charge of permissions, limits and approvals. The plugin's job is to translate between Arvel and the vendor's official CLI. This SDK does everything else.

> The SDK is Apache-2.0, so plugins built on it may use any licence, including closed source. (The Arvel app itself is AGPL-3.0; that does not extend to plugins, which run as separate processes.)
>
> **Status:** pre-1.0. The protocol (`pluginApi: "1"`) is stable; the Go API may still change between minor versions — see [CHANGELOG.md](CHANGELOG.md).

---

## What's inside

| Package | What it gives you |
|---|---|
| [`protocol`](protocol) | Go types for the Arvel Provider Protocol: `_meta.arvel` payloads, `_arvel/*` methods, error codes. |
| [`manifest`](manifest) | Load and validate `arvel.json`; JSON Schema in [`schema/`](schema). |
| [`plugin`](plugin) | The framework. Implement `Provider` and `Session`, call `plugin.Run`; or implement `Router`, call `plugin.RunRouter`. It handles transport, concurrency, cancellation, throttling, result validation and shutdown. |
| [`procx`](procx) | Cross-platform process handling: find a CLI, check its version, run it without a shell, kill the whole process tree (process groups on Unix, Job Objects on Windows). |
| [`jsonl`](jsonl) | Line-delimited JSON reading and writing with 64 MiB lines. |
| [`redact`](redact), [`logx`](logx) | Structured stderr logging with secret scrubbing. |
| [`testkit`](testkit) | Mock Arvel host, fake vendor CLIs that replay recorded output, and a YAML scenario format. |
| [`cmd/arvel-plugin-check`](cmd/arvel-plugin-check) | Conformance checker. Run it in CI before every release. |
| [`examples/echo`](examples/echo) | A complete minimal provider. Copy it to start a new plugin. |
| [`examples/echo-router`](examples/echo-router) | A complete minimal router with keyword rules. Copy it to start a router. |
| [`examples/team-sample`](examples/team-sample) | A declarative plugin with one role and one task type. |

## How a plugin works

```
┌──────────────┐  ACP over stdio (+ _arvel extensions)  ┌──────────────────────┐   vendor protocol   ┌─────────────┐
│  Arvel host  │ ─────────────────────────────────────▶ │  your plugin binary  │ ──────────────────▶ │ vendor CLI  │
│ (arvel-host) │ ◀───────────────────────────────────── │  (built on this SDK) │ ◀────────────────── │ (claude, …) │
└──────────────┘   status · models · usage · sessions   └──────────────────────┘                     └─────────────┘
```

- The wire protocol is the [Agent Client Protocol](https://agentclientprotocol.com). The plugin is an ACP agent and Arvel is the ACP client.
- Arvel-specific data travels in ACP extension points: `_meta.arvel` and `_arvel/*` methods. A plugin built with this SDK is a valid ACP agent everywhere.
- Full specification: [docs/PROTOCOL.md](docs/PROTOCOL.md).

## Quick start

These POSIX commands copy the provider's production files into a new module.
Use the corresponding copy commands and a `.exe` binary path on Windows.

```sh
git clone --depth 1 https://github.com/arvel-dev/arvel-sdk
mkdir arvel-plugin-myagent
cp arvel-sdk/examples/echo/main.go arvel-sdk/examples/echo/provider.go arvel-sdk/examples/echo/session.go arvel-plugin-myagent/
cp arvel-sdk/examples/echo/arvel.json arvel-plugin-myagent/
cp -R arvel-sdk/examples/echo/conformance arvel-plugin-myagent/
cp arvel-sdk/templates/LICENSE arvel-plugin-myagent/
cd arvel-plugin-myagent
go mod init github.com/you/arvel-plugin-myagent
go get github.com/arvel-dev/arvel-sdk@v0.1.2
go mod tidy
go build -o bin/plugin .
go install github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check@v0.1.2
arvel-plugin-check --level release --manifest arvel.json --scenarios conformance -- ./bin/plugin
```

The copied `main.go` checks the error returned by `plugin.Run`:

```go
func main() {
    if err := plugin.Run(context.Background(), newProvider(),
        plugin.WithVersion("arvel-plugin-myagent", version)); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

Implement `plugin.Provider` and `plugin.Session` following
[Writing a provider](docs/WRITING-A-PLUGIN.md). For routers, implement
`plugin.Router` plus optional routing interfaces and call `plugin.RunRouter`:
[Writing a router](docs/WRITING-A-ROUTER.md). To add roles, task types, MCP servers
or skills, follow [Writing a team or integration plugin](docs/WRITING-A-TEAM-PLUGIN.md).
The guides explain which files to copy and how to add tests outside this SDK.

## Publishing a plugin

1. Add `arvel.json` to the repository root (schema: [`schema/arvel.schema.json`](schema/arvel.schema.json)).
2. Copy the matching files from [`templates/`](templates): binary GoReleaser/CI/release files for providers and routers, or the data-only team workflow. Copy the release scripts and CHANGELOG skeleton too.
3. Add GitHub topics `arvel-plugin` and the type topic: `arvel-provider`, `arvel-router`, `arvel-team` or `arvel-integration`.
4. Record notes in a dated CHANGELOG version section, then tag `vX.Y.Z`. The workflow validates the package and creates a **draft**. Binary plugins have six platform archives, `checksums.txt` and a versioned `arvel.json`; teams have a data archive, manifest and checksums. Review the files and notes, then publish. Beta tags are prereleases.

Arvel finds published plugins through topic search. On install it verifies
checksums, shows requested permissions and runs binaries as separate processes.
Teams add declarative data; integration MCP servers run in the workspace sandbox.

## Conformance

```sh
arvel-plugin-check --level basic --manifest arvel.json -- ./bin/plugin
arvel-plugin-check --level release --manifest arvel.json --scenarios ./conformance -- ./bin/plugin
arvel-plugin-check --manifest ./examples/team-sample/arvel.json --package ./examples/team-sample
```

The last command runs from an SDK checkout. Add `--json` for a structured report.

| Mode | Checks |
|---|---|
| Provider `basic` | Manifest, initialize under 5 s, status/models shapes, unknown methods, clean shutdown, valid stderr JSON and canary-secret leakage across raw stdout/stderr. |
| Provider `release` | Basic plus scenarios, concurrent sessions, cancellation, busy-session error and fake-vendor proxy forwarding. |
| Router | Router initialize/capabilities, rejection of provider sessions, status and leakage; release adds scenarios and 100 seeded request-validity/deadline probes with plugin slots. |
| Declarative | Manifest, contributions and package files; integration `release` starts stdio MCP servers and lists tools. |

Skips are reported explicitly: fast echo turns may finish before cancel/busy,
providers without a proxy scenario skip that probe, and remote MCP tool listing
is not performed. A green report does not establish a skipped guarantee.

## Compatibility

| | |
|---|---|
| Go | 1.27.2 or newer (module minimum); release builds need no CGO |
| Platforms | macOS, Linux and Windows on amd64 and arm64 |
| Protocol | `pluginApi: "1"` |

## Versioning

The SDK follows semver. The protocol (`pluginApi: "1"`) does not break; the Go API may change in minor releases before 1.0, and every change is in [CHANGELOG.md](CHANGELOG.md) with a migration note. Pin an exact version in your `go.mod`.

## Development

From the repository root (use `.exe` filenames on Windows):

```sh
go test -race ./...
go build -o bin/arvel-plugin-check ./cmd/arvel-plugin-check
go build -o bin/echo ./examples/echo
go build -o bin/echo-router ./examples/echo-router
./bin/arvel-plugin-check --level release --manifest examples/echo/arvel.json --scenarios examples/echo/conformance -- ./bin/echo
./bin/arvel-plugin-check --level release --manifest examples/echo-router/arvel.json --scenarios examples/echo-router/conformance -- ./bin/echo-router
./bin/arvel-plugin-check --manifest examples/team-sample/arvel.json --package examples/team-sample
```

Race tests require a C compiler; distributable binaries are built with
`CGO_ENABLED=0`. Tag releases run the three-OS quality gate, conformance and
six-target checker packaging before creating a draft. Release notes come only
from the version's CHANGELOG section through `scripts/release-notes.sh`.

## License

Apache-2.0. See [LICENSE](LICENSE). The code in `examples/` and `templates/` is MIT-0: copy it into your plugin without attribution.

Arvel is an independent project. Names of AI vendors and their products belong to their respective owners.
