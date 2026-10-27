# arvel-plugin-example

[![Release](https://img.shields.io/github/v/release/YOUR_ORG/arvel-plugin-example)](https://github.com/YOUR_ORG/arvel-plugin-example/releases)
[![CI](https://github.com/YOUR_ORG/arvel-plugin-example/actions/workflows/ci.yml/badge.svg)](https://github.com/YOUR_ORG/arvel-plugin-example/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT--0-blue.svg)](LICENSE)

Suggests task types, role participation, message destinations and reasoning
effort for Arvel. Name exactly which of those decisions this router implements.

## What you need

List the required account/service or state that routing is local. Give verified
setup commands for each OS and any minimum service/CLI versions.

## Decisions

| Feature | Behavior | Fallback when unavailable |
|---|---|---|
| `route.plan` | Fill with task-type/slot selection rules | Host keeps the task type defaults |
| `route.message` | Fill with candidate selection rules, or remove this row | Host keeps the current role |
| `route.effort` | Fill with supported levels, or remove this row | Host keeps the configured effort |

`route.explain` is optional. The host owns built-in role order, ignores decisions
on `auto` slots and a type's `lead_accept`, and applies its confidence policy.

## Settings and connections

Document each connection kind, credentials, billing, routing model, confidence
behavior and configurable timeouts. English names/descriptions are required;
additional UI languages are optional. Settings must match `arvel.json`.

## Privacy: what is sent

The host sends **only**: task title and description, the message text (for `route.message`), project names, the UI language, the task types (id, name, description, default roles) and for each role: `roleId`, name, description ("when this role is needed", ≤ 300 characters) and kind. **Never** code, diffs, file names, file contents, feed history, knowledge files, task codes from trackers or secrets of other plugins. The router MUST NOT ask for more and MUST NOT store request text beyond the request (debug logs follow §10).

This paragraph is the privacy contract from the SDK's PROTOCOL §14.2. Add the
service's retention/telemetry policy and what this plugin logs. Do not broaden
the host payload or request workspace files to improve routing.

## Failure behavior

Route calls have a 3-second host deadline; the SDK framework defaults to 2.5
seconds. Describe service errors, rate limits and retry behavior within that
budget. Unknown features return method-not-found; invalid decisions fail
validation. The router never supplies provider sessions.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Suggestions absent | Timeout, low confidence or no matching rule | Inspect redacted logs and documented settings |

## Compatibility

State `pluginApi: "1"`, tested service versions, pinned SDK version and all six
darwin/linux/windows × amd64/arm64 targets.

## Disclaimer

Independent project, not affiliated with service vendors; trademarks belong to
their owners.

## Development

Copy production files `main.go`, `router.go`, `rules.go`, the manifest and
`conformance/` from `examples/echo-router`. Do not copy tests importing the SDK's
internal packages. Use the public mockhost API in your own tests. Follow
`docs/WRITING-A-ROUTER.md` in the SDK for the optional interfaces and validation.

Use the binary packaging/CI/release templates and both scripts, then replace
`project_name`, binary, all manifest asset entries and plugin identity together.
The provider `templates/arvel.json` is not a router manifest: start with the
router example's manifest instead. Pin checker `v0.1.2`.

```sh
go build -o bin/plugin .
go test -race ./...
arvel-plugin-check --level release --manifest arvel.json --scenarios conformance -- ./bin/plugin
```

Tag a dated CHANGELOG version to create a draft; inspect six archives, the
versioned manifest and checksums before publishing. Beta tags become
prereleases. Add topics `arvel-plugin` and `arvel-router`.

## License

Name your license and include LICENSE. SDK example/template code is MIT-0.
