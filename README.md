# Arvel SDK

[![CI](https://github.com/arvel-dev/arvel-sdk/actions/workflows/ci.yml/badge.svg)](https://github.com/arvel-dev/arvel-sdk/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/arvel-dev/arvel-sdk.svg)](https://pkg.go.dev/github.com/arvel-dev/arvel-sdk)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

The Go SDK for writing **plugins for [Arvel](https://github.com/arvel-dev/arvel)**, and the conformance tests every plugin must pass. Two kinds are supported:

- **provider plugins** connect Arvel to an AI coding agent;
- **router plugins** help Arvel decide which roles of a team take part in a task (for example [arvel-plugin-jev](https://github.com/arvel-dev/arvel-plugin-jev)). Without one, Arvel simply follows the team's order.

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

```bash
go install github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check@latest
git clone --depth 1 https://github.com/arvel-dev/arvel-sdk && cp -R arvel-sdk/examples/echo ./arvel-plugin-myagent
cd arvel-plugin-myagent && go mod init github.com/you/arvel-plugin-myagent && go get github.com/arvel-dev/arvel-sdk@latest
```

```go
package main

import (
    "context"
    "os"

    "github.com/arvel-dev/arvel-sdk/plugin"
)

func main() {
    os.Exit(plugin.Run(context.Background(), NewProvider(), plugin.WithVersion("arvel-plugin-myagent", version)))
}
```

Then implement `plugin.Provider`, which covers status, models, usage, actions and sessions, plus `plugin.Session`, which runs one turn at a time and handles cancel. [docs/WRITING-A-PLUGIN.md](docs/WRITING-A-PLUGIN.md) walks through the echo example line by line. For a router, start from [docs/WRITING-A-ROUTER.md](docs/WRITING-A-ROUTER.md): implement any of `Plan`, `Message`, `Effort`; the framework checks that you only answer about the steps and roles you were given.

## Publishing a plugin

1. Add `arvel.json` to the repository root (schema: [`schema/arvel.schema.json`](schema/arvel.schema.json)).
2. Copy the files from [`templates/`](templates): GoReleaser config, CI and release workflows.
3. Add the GitHub topics `arvel-plugin` and `arvel-provider` (or `arvel-router` for a router).
4. Tag a release `vX.Y.Z`. The release workflow runs `arvel-plugin-check --level release` and then publishes binaries for macOS, Linux and Windows (amd64 and arm64), `checksums.txt` and `arvel.json`.

Arvel finds your plugin through the topic search. On install it verifies checksums, shows the permissions your manifest asks for, and runs your binary as a separate process.

## Conformance

```bash
arvel-plugin-check --level basic -- ./dist/arvel-plugin-myagent
arvel-plugin-check --level release --scenarios ./conformance -- ./dist/arvel-plugin-myagent
```

| Level | Checks |
|---|---|
| `basic` | Manifest validity, initialize under 5 s, status and models shapes, unknown methods, clean shutdown, no secret leakage. A canary secret is injected and all output is searched for it. |
| `release` | Everything in `basic`, plus your scenarios, two sessions in parallel, cancellation during a turn, and the busy-session error. |

## Compatibility

| | |
|---|---|
| Go | 1.26 or newer, no CGO |
| Platforms | macOS, Linux and Windows on amd64 and arm64 |
| Protocol | `pluginApi: "1"` |

## Versioning

The SDK follows semver. The protocol (`pluginApi: "1"`) does not break; the Go API may change in minor releases before 1.0, and every change is in [CHANGELOG.md](CHANGELOG.md) with a migration note. Pin an exact version in your `go.mod`.

## Development

```bash
go test -race ./...
go run ./cmd/arvel-plugin-check --level release --scenarios ./examples/echo/conformance -- go run ./examples/echo
```

## License

Apache-2.0. See [LICENSE](LICENSE). The code in `examples/` and `templates/` is MIT-0: copy it into your plugin without attribution.

Arvel is an independent project. Names of AI vendors and their products belong to their respective owners.
