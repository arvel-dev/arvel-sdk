# arvel-plugin-example

[![Release](https://img.shields.io/github/v/release/YOUR_ORG/arvel-plugin-example)](https://github.com/YOUR_ORG/arvel-plugin-example/releases)
[![CI](https://github.com/YOUR_ORG/arvel-plugin-example/actions/workflows/ci.yml/badge.svg)](https://github.com/YOUR_ORG/arvel-plugin-example/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT--0-blue.svg)](LICENSE)

Connects Arvel to YOUR_AGENT through its official CLI. Replace every YOUR value
and the example identifiers before publishing. Keep this README specific to the
CLI versions you have actually tested.

## What you need

List the vendor CLI, minimum supported version, official installation links and
verified install commands for macOS, Linux and Windows. Explain whether a remote
Arvel host needs the CLI installed on that host. State required accounts or plans.

## Connections

| Kind | Authentication | Billing | Limits or spend Arvel can show |
|---|---|---|---|
| YOUR_KIND | Describe the source of credentials | Subscription/API/local | Exact, estimated or unavailable |

## Signing in

Give verified steps for each connection kind, including a headless remote host.
Identify where the CLI stores credentials and how to sign out. Never ask readers
to paste credentials into a tracked file or a bug report.

## Models & reasoning effort

Explain how the model list is obtained, its refresh behavior, default model and
the mapping from Arvel's `low`, `medium`, `high`, `xhigh`, `max` vocabulary to the
CLI's supported values. Do not advertise unsupported levels.

## Permissions & safety

Describe reads, edits, commands, network and subagents. State which restrictions
the CLI enforces, which operations ask Arvel, and how a rejection or cancellation
stops the operation. Document sandbox vendor paths/hosts, process cleanup and the
limits of enforcement. A permission declaration must match actual behavior.

## Usage & limits

For each displayed number, identify its source, refresh rate, accuracy and what
"unknown" means. Distinguish subscription windows from API spend estimates.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| CLI not found | Missing installation or host PATH | Use the verified installation steps above |
| Authentication required | Connection is signed out | Follow Signing in for that connection kind |

Add tested CLI-specific failures without including tokens or private prompts.

## Privacy

List exactly what goes to the vendor, where it goes and why. Describe retention
and telemetry using the vendor's official sources. Explain the JSON stderr logs,
`ARVEL_LOG_LEVEL`, secret redaction and whether debug logs contain truncated text.
Do not claim that no data leaves the machine if the CLI sends requests remotely.

## Compatibility

| Protocol | Go | Platforms | Tested CLI versions |
|---|---|---|---|
| `pluginApi: "1"` | Same minimum as pinned SDK | darwin/linux/windows × amd64/arm64 | Fill from recorded fixtures and CI |

## Disclaimer

Independent project, not affiliated with the vendor. Product names and trademarks
belong to their respective owners.

## Development

Start with the SDK's provider guide. Copy only `main.go`, `provider.go`,
`session.go`, `arvel.json` and `conformance/` from `examples/echo`; the SDK tests
have repository-specific build targets. Add your own tests using public testkit
APIs. Rename the plugin in `main.go`, the manifest and `.goreleaser.yaml` together.
Pin the SDK and checker to `v0.1.1`.

```sh
go build -o bin/plugin .
go test -race ./...
arvel-plugin-check --level release --manifest arvel.json --scenarios conformance -- ./bin/plugin
```

Copy `.github/workflows/ci.yml`, `.github/workflows/release.yml`,
`.goreleaser.yaml`, `scripts/prepare-release.sh`, `scripts/release-notes.sh` and
`CHANGELOG.md` from the SDK templates. Ignore `/bin/`, `/dist/` and `/.release/`.
The release hook stages a versioned manifest without editing the source.

Before tagging, move the notes from Unreleased into a dated version section.
Tag `vX.Y.Z`; CI tests conformance and creates a **draft** with six archives,
`arvel.json` and `checksums.txt`. Review notes and hashes, then publish. A beta tag
such as `v0.2.0-beta.1` creates a prerelease draft. Add GitHub topics `arvel-plugin`
and `arvel-provider` for discovery.

## License

Choose and name the plugin's license. The SDK's examples and templates are MIT-0;
the SDK library is Apache-2.0. Keep the license used by your release in LICENSE.
