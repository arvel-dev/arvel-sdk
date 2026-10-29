# Writing a provider plugin

A provider translates between Arvel's ACP connection and an agent CLI. The
SDK owns framing, dispatch, session concurrency, result validation and shutdown;
your provider owns the CLI process, authentication and permission enforcement.
The wire contract is [PROTOCOL.md](PROTOCOL.md). Router and declarative plugins
have [separate](WRITING-A-ROUTER.md) [guides](WRITING-A-TEAM-PLUGIN.md).

## Start with echo

[`examples/echo`](../examples/echo) has no external dependencies or credentials.
It is a runnable provider with one `local` connection and model `echo-1`.
From a directory containing a clone of this SDK, these POSIX commands create a
separate module. Go must meet the SDK's `go.mod` minimum (currently 1.27.2).

```sh
mkdir arvel-plugin-myagent
cp arvel-sdk/examples/echo/main.go arvel-sdk/examples/echo/provider.go arvel-sdk/examples/echo/session.go arvel-plugin-myagent/
cp arvel-sdk/examples/echo/arvel.json arvel-plugin-myagent/
cp -R arvel-sdk/examples/echo/conformance arvel-plugin-myagent/
cp arvel-sdk/templates/LICENSE arvel-plugin-myagent/
cd arvel-plugin-myagent
go mod init github.com/YOUR_ORG/arvel-plugin-myagent
go get github.com/arvel-dev/arvel-sdk@v0.2.0
go mod tidy
go build -o bin/plugin .
go install github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check@v0.2.0
arvel-plugin-check --level release --manifest arvel.json --scenarios conformance -- ./bin/plugin
```

On Windows, use your shell's copy commands and build `bin/plugin.exe`, then pass
that path to the checker. Copy production files explicitly: the SDK's example
tests contain repository-specific build targets and internal helpers. Write
tests for your new module instead. Before the first SDK publication, contributors
can use a temporary `go mod edit -replace` pointing at their SDK checkout; remove
that replacement before publishing a plugin.

## Entry point and provider lifecycle

[`main.go`](../examples/echo/main.go) serves one stdio connection:

```go
func main() {
    if err := plugin.Run(context.Background(), newProvider(),
        plugin.WithVersion("arvel-plugin-myagent", version)); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

`Run` returns an error. Keep stdout exclusively for protocol messages. The
example declares `version`, which the release template overwrites with ldflags.
Rename the plugin identity here and its manifest/asset names together.

[`provider.go`](../examples/echo/provider.go) implements `plugin.Provider`:

| Method | Responsibility |
|---|---|
| `Initialize` | Accept `protocol.InitMeta`, return `plugin.Features`; under five seconds, no network |
| `Status` | Report ready/auth-required/error and actionable status |
| `Models` | Return unique model IDs, supported efforts and an existing default |
| `Usage` | Return measured, estimated or explicitly unknown limits/spend |
| `Action` | Run an action previously advertised in status |
| `NewSession` | Create a stable session ID from `plugin.SessionParams` |
| `LoadSession` | Resume by `plugin.LoadParams.SessionID` when `Features.LoadSession` is true |
| `Shutdown` | Release provider resources after sessions close |

Echo stores sessions in memory, announces resume and returns deterministic model
and fake usage data. Replace those numbers with real measurements or unknowns.
Implement `plugin.HostAware.SetHost(plugin.Host)` if you need status/usage
notifications or the redacting logger; the framework calls it before Initialize.
The five-second initialization deadline does not include slow vendor checks in
`Status`; honor each request context and use a separate bounded status wait.
Identical consecutive status notifications are dropped, and usage notifications
are throttled to one per five seconds with the last value retained.

Optional `plugin.Discoverer` enables `_arvel/discover`. Return secret/envRef
references for discovered credentials, never credential values. Optional
`plugin.ConfigOptioner` on a session supplies model/thought-level config options.

## Verify recovery across process lifetimes

A repeated `session/load` in one SDK server can return an already registered
session without calling the provider's `LoadSession` again. Test a fresh plugin
server and vendor process as well: initialize with the same explicit connection
and private data directory, load the original session ID and cwd, then send a
new prompt. Assert that only the new turn produces live output, usage and
permission decisions, that cancellation still works, and that the original
permissions/MCP/skills scope remains effective.

Add deterministic fake-vendor cases for delayed old text, lifecycle, usage and
approval events around resume and the next turn-start reply. Restore historical
identities using the vendor's supported response fields; keep unknown-identity
failures strict. No additional Arvel wire field is needed for vendor turn IDs.
Record a bounded opt-in native restart test separately from SDK/mock-host tests.
Never copy shared authentication to make recovery pass. See
[PROTOCOL §7.4](PROTOCOL.md#74-sessionload-resume).

The testkit checks fresh SDK-server dispatch with a stable environment. It does
not certify a provider's vendor transport or native persistence. Echo's in-memory
example is a wire demonstration; use your provider's own durable vendor state
for production recovery.

## One session, one running turn

[`session.go`](../examples/echo/session.go) implements `plugin.Session`: stable
`ID`, `Prompt`, `SetOption`, nonblocking/idempotent `Cancel`, and `Close`.
`Prompt(ctx, blocks, out)` streams ACP updates through `out.Update` and returns a
`plugin.TurnResult` with a valid ACP stop reason. Echo streams words and reports
illustrative per-turn usage. Session metadata carries model, effort, resolved
permissions, task/read-only state, MCP servers and skills; consume those settings
instead of relying on role display names.

`session/cancel` calls `Cancel`; it does **not** cancel Prompt's context. Signal
your worker yourself, stop the vendor gracefully and return `cancelled`. The
context ends on shutdown/disconnection. The provider owns escalation to a
process-tree kill for stuck work. Every goroutine needs an owner that waits for
it. `SetOption` affects the next turn and must not interrupt the current one.
`Close` releases the session within its deadline. The framework rejects a second
prompt on the same running session with `busy`; distinct sessions may run together.

## Permissions, processes and skills

Echo's `!run` path demonstrates `Output.RequestPermission`; it does not execute
a command. A real provider must honor `PermissionDecision.Cancelled` and reject
options, and act only on the selected allowed option. Permission waits have no
timeout and end when the session is cancelled. Respect read-only tasks: their
resolved edit permission is deny. The SDK does not make an unsafe CLI safe by
itself; translate every permission into the CLI's actual supported controls.

Use `procx.FindBinary`, `Version` and `AtLeast` for recorded CLI requirements.
Start children with `procx.Start`, a workspace directory and `procx.Env().Build()`;
avoid shell commands. When `InitMeta.Sandbox` is non-nil, pass its **proxy string**
to `procx.Env().Proxy(init.Sandbox.Proxy)`. The builder strips sensitive parent
variables such as `SSH_AUTH_SOCK` and `GH_TOKEN`. Add vendor secrets explicitly
only where the protocol authorizes them. Processes are scoped to one connection
and workspace; do not share one vendor process between workspaces.

`protocol/skills.Materialize(cwd, dirs, refs)` returns `(cleanup func(), err error)`.
Use the announced project skill directories and the host's refs, retain cleanup
for session Close, and run it on failed setup too. It handles git/worktree
excludes, name collisions and manual-invocation metadata. Cleanup is idempotent
and checks a unique copy owner before deletion. Existing marked copies are
preserved: a marker does not prove that another session has ended. If both the
requested name and its `arvel-` form are occupied, setup returns `ErrNameTaken`.
Do not reclaim those folders automatically. Use separate session working
directories or a verified native per-session directory mechanism when selections
differ or names are exhausted; shared project directories expose their skills
to every CLI session using that directory.

The host keeps a separate shared catalogue for each provider, with explicit
source and owner; spaces and roles select skills for that provider. Consume only
the resolved enabled Arvel refs in session metadata. `manual` controls automatic
invocation, not whether a skill is enabled. Disabling/removing an Arvel skill in
a space affects subsequent sessions or a safe session restart, never its external
original or copies owned by other active tasks.

External/global CLI skills may remain available. Implement CLI-specific controls
in the provider and document which ones work; the host must remain independent
of vendor paths and flags. If exclusion is unsupported, the application must
disclose that those skills remain available and omit a nonfunctional toggle.
Use the existing connection config schema/status information (§5.1, §6) and plugin
documentation to describe supported behavior; no new skills wire fields are
needed. Skill visibility does not change file/command/network permissions,
authentication isolation, the sandbox or the MCP policy. See
[PROTOCOL §7.7](PROTOCOL.md#77-provider-skill-catalogues-and-the-hosts-mcp-catalogue).
The ownership-safe cleanup described here requires SDK v0.1.3 or later; the Go
API and `pluginApi: "1"` wire fields are unchanged.

## Manifest and logs

[`arvel.json`](../examples/echo/arvel.json) declares identity, version, pluginApi,
connections, capabilities, permissions and six platform entries. Every required
CLI must appear in both `requires` and `permissions.spawn`. Declare sandbox vendor
paths/hosts for actual access; the manifest is reviewed before installation.
Keep the binary's stdout clean. Use the host's `log/slog` logger for JSON stderr,
respect `ARVEL_LOG_LEVEL`, and never log credentials. Avoid full prompts above
debug; debug text must be truncated and redacted. Use `protocol.NewError` for
typed wire failures and wrap other Go errors with useful context.

## Tests and release checks

Add this external-safe test as `provider_test.go` in the copied module:

```go
package main

import (
    "testing"
    "github.com/arvel-dev/arvel-sdk/testkit/mockhost"
)

func TestBasicTurn(t *testing.T) {
    h := mockhost.InProcess(t, newProvider())
    h.RunScenario(t, "conformance/basic-turn.yaml")
    h.Shutdown()
}
```

Echo's `basic-turn.yaml` verifies status, models, a session and streamed output;
`permission.yaml` answers the permission request once. Extend scenarios for real
CLI behavior using `testkit/fakevendor` scripts derived from recorded, scrubbed
fixtures. Unit tests must not depend on real credentials or network access.

`basic` checks startup, shapes, unknown methods, shutdown and secret leakage.
`release` adds all scenarios, concurrent sessions, cancel, busy and a proxy check.
The checker reports skips when the echo turn finishes too fast for cancel/busy
or no fake-vendor proxy scenario exists; a green result does not prove skipped
paths. Supply those fixtures for a vendor-spawning provider.

For declared connections requiring test config or synthetic secrets, use
[`--test-env`](CONFORMANCE-ENVIRONMENT.md). It supplies consistent initialization
to every applicable check, supports scenario overrides and prepares `plugin.env`
before startup. Configure streaming, unfinished turns for cancel/busy: explicit
mode fails when those assertions cannot be exercised. Testkit callers can use
`NewEnvironment` and `PrepareScenario`; existing host method signatures remain.

Run `go test -race ./...` with a working C compiler, then build releases with
`CGO_ENABLED=0`. Copy the [provider README](../templates/README.plugin.md), binary
CI/release workflows, `.goreleaser.yaml`, manifest and both scripts from
[`templates`](../templates). Preserve your provider's own manifest when adapting
the generic template. Pin SDK/checker versions, rename all asset entries, ignore
generated `bin/`, `dist/`, `.release/`, and record changes in CHANGELOG. Tag only a
dated, nonempty version section; review the resulting draft before publishing.
