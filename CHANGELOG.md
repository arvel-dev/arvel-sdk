# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows
[Semantic Versioning](https://semver.org/). Before v1.0.0 a minor release may
change the Go API; every such change carries a migration note. The protocol
(`pluginApi: "1"`) never breaks.

## [Unreleased]

## [0.1.1] - 2026-10-28

### Added
- First published GitHub release with six-platform checker archives and
  SHA-256 checksums.

### Fixed
- `arvel-plugin-check` resolves relative executable paths and PATH commands
  before launching provider/router checks, scenarios and proxy probes.
- Standalone guide and release smoke tests now exercise relative binary paths;
  copied workflows and guides pin the corrected SDK/checker version.

## [0.1.0] - 2026-10-28

The tag exists, but release validation stopped before a GitHub release was
created. Use v0.1.1 for the corrected checker and publishing workflow.

### Added
- Author guides for provider, router, team and integration plugins, with
  standalone examples and pinned SDK setup instructions.
- Copyable manifests, README skeletons, CI and draft-release workflows for
  binary and data-only plugins, plus exact versioned CHANGELOG extraction.
- Six-platform `arvel-plugin-check` release archives with SHA-256 checksums;
  release automation validates examples and creates a draft for review.
- `testkit/mockhost` exposes captured raw protocol output through `Stdout()`.
- Go module `github.com/arvel-dev/arvel-sdk` with a documented root package.
- `docs/PROTOCOL.md`: the Arvel Provider Protocol v1, now the source of truth.
- CI on Linux, macOS and Windows (`go vet`, `staticcheck`, `go test -race`) and a
  check that every `.go` file carries an SPDX licence header.
- `docs/FINDINGS.md`: ACP compatibility findings. The
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

- `jsonrpc` package: a bidirectional line-delimited JSON-RPC 2.0 peer that accepts
  lines up to 64 MiB, survives an over-long line, handles requests concurrently
  and delivers the notifications of a session in order, before the response that
  follows them.
- `plugin` package: `Run` for provider plugins and `RunRouter` for router
  plugins. They answer `initialize`, the `_arvel/*` methods and the ACP session
  methods, validate every result, redact secrets from logs and errors, throttle
  usage updates, close idle sessions and reload them on demand, and shut down
  cleanly on `_arvel/shutdown`, end of input or a signal.
- `protocol/skills` package: `Materialize` copies the host's skill folders into
  the vendor CLI's project skills directory, hides them from git and removes them
  when the session ends.

- `protocol`: `TaskMeta` (`task` in `_meta.arvel` of `session/new`), the `RoleKind`
  and `SlotState` enums, constants for the built-in roles, slots and task types,
  `Order`, `IsOrderSlot`, `PluginSlot`, and `SessionMeta.Normalize`, which makes a
  read-only task always deny edits.
- `protocol`: `RoutePlanResult.Sanitize` drops the decisions the host ignores (auto
  slots and a type's `lead_accept`) and reports them.
- `manifest`: plugin types `team` and `integration`, the `contributes` block (roles,
  task types, MCP servers, skills) with validation, `Manifest.ValidateFiles` for
  instruction and skill files, and matching rules in `schema/arvel.schema.json`.
- `plugin`: `RunRouter` drops ignored decisions and logs them at `warn`; `Run` makes
  sure a read-only task never has edit access.
- `examples/echo-router` and `examples/team-sample`.

- `testkit/scenario` package: the YAML scenario format shared by tests and
  `arvel-plugin-check` (value templating, JSONPath saves, subset assertions).
- `testkit/fakevendor` package: builds a fake vendor CLI from a script, for
  testing a provider without the real vendor CLI.
- `testkit/mockhost` package: a scripted ACP client that drives a provider or
  router in-process or as a binary and runs scenario files.
- `examples/echo`: an in-memory provider (kind `local`, model `echo-1`) that
  streams word by word, asks permission on `!run`, and supports cancel and resume.
- `cmd/arvel-plugin-check`: the conformance CLI. `--level basic`
  and `--level release` for provider and router plugins, `--manifest`/`--package`
  for declarative `team`/`integration` plugins, `--scenarios` for YAML scenario
  runs and `--json` for a machine-readable report; the exit code is 0 when every
  non-skipped check passed, 1 on any failure and 2 on a usage error. Provider
  `basic` validates the manifest, bounds `initialize` at 5 s, checks the
  `_arvel/status` and `_arvel/models` shapes, expects `-32601` for an unknown
  `_arvel/*` method, requires `_arvel/shutdown` to exit 0 within 10 s, checks
  that stderr is JSON lines and injects a canary secret into `initialize` and
  greps all captured output for it. `release` adds the `--scenarios` runs,
  concurrency, cancel and busy probes and the sandbox proxy test. Routers get
  the router conformance suite (empty `agentCapabilities`, router-only features,
  `session/new` → `-32601`, id/slot/confidence validity, a 100-request randomized
  validity check under a 3 s deadline). `--manifest`/`--package` runs declarative
  checks with no binary; `release` for an `integration` plugin also starts a
  stdio MCP server and lists its tools.
- `testkit/mockhost`: `Driver` and `StartDriver`, an error-returning binary
  driver for non-test callers (the `testing.TB`-bound `Host` is unchanged), and
  `Driver.RunScenario` with the structured `ScenarioResult`/`StepResult`.
- `testkit/fakevendor`: an `emit: {env: NAME}` script step, so a conformance
  scenario can make the fake vendor report an environment variable.

- New exported packages `protocol`, `manifest`, `schema`, `jsonl`, `redact`,
  `logx`, `procx`, `jsonrpc`, `plugin` and `protocol/skills`. Except for the items
  marked **BREAKING:** under Changed and Removed, all changes are additive.
- New exported packages `testkit/scenario`, `testkit/fakevendor` and
  `testkit/mockhost`, and the `examples/echo` provider (MIT-0).
- New exported API of `protocol` and `manifest` as listed under Added.
- New dependencies: `github.com/santhosh-tekuri/jsonschema/v6` and
  `golang.org/x/mod` (plus `golang.org/x/text`, which `jsonschema` needs), and
  `golang.org/x/sys` (used only by the Windows code of `procx`).
- New dependency: `github.com/coder/acp-go-sdk` (ACP schema types only; it adds no
  further modules).
- New dependency: `gopkg.in/yaml.v3` (the YAML scenario format of `testkit`;
  imported only by `testkit` and `cmd`, not by the runtime plugin packages).

### Changed
- `docs/PROTOCOL.md` synced with the workspace sandbox: new §15 Workspace
  sandbox, the manifest `sandbox` block, the `initialize` `sandbox` field, one
  provider process per connection and workspace, provider security rule 6 and the
  conformance note. The change is additive within `pluginApi: "1"`.
- `docs/PROTOCOL.md` synced with the protocol of 27 October 2026: task types and
  slots in the router calls, `task` in `session/new`, declarative plugins (§16), no
  built-in plugins, `git_push` and `pr_open` limited to the lead role.
- **BREAKING:** the router wire types speak task types and slots. `RouteStep`,
  `RoutePreset`, `PresetDecision`, `StepDecision`, `WorkKind`, `Participation`,
  `RoutePlanParams.Steps` and `.Presets`, `RoutePlanResult.Preset` and `.Steps`, and
  `IssueNotDecidable` are replaced by `RouteType`, `RouteRole`, `TypeDecision`,
  `RoleDecision`, `RoleKind`, `SlotState`, `RoutePlanParams.Types` and `.Roles`, and
  `RoutePlanResult.Type` and `.Roles`. Migration: build `types` from the host's task
  types and `roles` from its slots (a role added by a plugin has the slot
  `<pluginId>:<roleId>`); answer with a `TypeDecision` and `RoleDecision`s keyed by
  slot.
- **BREAKING:** `RouteCandidate.Kind` and `RouteEffortParams.Kind` have the type
  `RoleKind` (was `WorkKind`). Migration: use `RoleKindCheck` where you used
  `review` or `verify`, `RoleKindIntake` for `spec`, and `RoleKindAccept` for the
  lead's acceptance.
- **BREAKING:** `manifest.PluginType.Valid` also accepts `team` and `integration`,
  and `Manifest.Entry` and `Manifest.Connections` are omitted from JSON when empty.
  Migration: none for provider and router manifests.
- `RoutePlanResult` and `RouteExplainResult` always write their lists as arrays.

### Fixed
- Conformance checks scan raw stdout as well as stderr for secret leakage, run
  two sessions concurrently, and exercise plugin slots and varied task rosters
  while respecting a router's announced features. Router initialization and
  unknown-method probes now have bounded waits.
- `arvel-plugin-check --help` exits successfully.
- Fake vendor builds, binary conformance tests and packaged MCP lookup use
  executable filenames with `.exe` on Windows.

### Removed
- **BREAKING:** interactive terminal sessions: `InteractiveResult`,
  `InteractiveState` (with `InteractiveStateIdle`, `InteractiveStateBusy` and
  `AllInteractiveStates`), `MethodInteractive`, `FeatureInteractive`, and the
  `Interactive` and `State` fields of `UpdateMeta`. The method `_arvel/interactive`
  and the feature `interactive` are reserved. Migration: delete the code that used
  them.
- The manifest type `tool` is rejected; use `integration`.
