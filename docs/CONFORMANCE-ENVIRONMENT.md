# Conformance test environments

`arvel-plugin-check` supplies host inputs for tests. These inputs are independent
of production configuration and never authorize a login, a paid request or access
to a shared credential store. Use synthetic secrets, built fake executables and
test-controlled endpoints.

## Running the checker

```sh
arvel-plugin-check --level release --manifest arvel.json \
  --scenarios conformance --test-env conformance-env.yaml -- ./bin/plugin
```

`--test-env` accepts one YAML document (at most 1 MiB), rejects unknown fields,
and keeps initialization, startup environment and probe inputs in one file.
For example, a synthetic test provider could use:

```yaml
initialize:
  connection:
    kind: synthetic
    config:
      executable: "{{fakevendor:dependency}}"
      enabled: false
      count: 2
    secrets:
      token: synthetic-test-token
plugin:
  env:
    FIXTURE_MODE: normal
checks:
  concurrency:
    plugin: {env: {FIXTURE_MODE: interleave}}
    prompt: [{type: text, text: overlap}]
  cancel:
    plugin: {env: {FIXTURE_MODE: hold}}
    prompt: [{type: text, text: wait}]
  busy:
    plugin: {env: {FIXTURE_MODE: hold}}
    prompt: [{type: text, text: wait}]
```

The names and modes above are inputs understood by that hypothetical fixture,
not presets understood by the SDK. Supply inputs your own test executable
implements. Fake scripts live at `testdata/fakevendor/<name>.yaml`, beside the
`conformance` directory. Building a fake compiles its executable; it does not
execute the dependency. Building fakes requires Go and SDK sources: source builds
use their SDK checkout; downloaded/trimmed binaries resolve the SDK selected by
the current consumer Go module, honoring its local replace or module cache. Run
from that module; unavailable sources produce a configuration failure. The Go
compiler process tree is stopped and joined on cancellation. Initialization must still answer within five seconds
without running an external CLI or contacting a network service.

The root fields are `initialize`, `workspace`, `plugin` and `checks`.
`initialize` accepts `connection` (`id`, `kind`, `config`, `secrets`) and `dataDir`.
`plugin` accepts `env`. Named checks are `basic`, `concurrency`, `cancel`, `busy`,
`route-validity` and `proxy`. Each accepts `initialize`, `workspace` and `plugin`;
only the three provider session probes accept `prompt`, an ACP content-block
array. There are no check-disabling or capability-masking settings.

Provider and router manifests declare connection kinds, not authenticated test
instances. Without explicit selection, the checker selects a kind only when
there is exactly one, all declared config fields have defaults and no secret
is required. Otherwise supply `initialize.connection.kind` and the necessary
test inputs. The checker validates the chosen declaration's primitive types,
enum/pattern/bounds and required secrets. The manifest schema has no config
`required` list; requirements outside that declaration are still enforced by
the plugin. A non-ready connection fails before the built-in session probes;
the checker never runs a login action to make it ready.

Router checks use connection initialization and routing capabilities, without
provider session probes. Team/integration package checks keep their declarative
contract and applicable integration MCP checks; `--test-env` is not a package-mode
option.

## Precedence and isolation

From lowest to highest priority:

1. Generated host metadata, private data/workspace paths and manifest config defaults.
2. Root settings in `--test-env`.
3. The named check's settings, for a built-in check.
4. The selected scenario's `initialize`, `workspace` and `plugin.env`.
5. The checker's fresh canary secret and, in the proxy probe, its sandbox proxy.

Ordinary scenarios do not inherit `checks.proxy`. The dedicated proxy run applies
root settings, `checks.proxy`, then its selected scenario. Each scenario gets a
fresh process and generated directories; it may select a different connection.
The checker does not impose a single connection on every scenario.

For the same kind, config and secrets merge by key. An omitted map inherits;
an explicit `{}` clears inherited test values. Manifest defaults are then
applied to missing config fields. A changed kind replaces the whole inherited
connection, so config and secrets never carry across kinds. An explicitly empty
kind is an error. Startup env merges by variable name, using Windows case rules
on Windows; duplicate native names within a single input map are errors.

Every independent driver gets separate, absolute private data and workspace
directories. New/load operations on one driver share its workspace. The plugin
process starts in its data directory, with matching `ARVEL_PLUGIN_DATA_DIR`.
Explicit `dataDir` and `workspace` must be absolute existing directories. They
are borrowed: the checker never chmods or removes them. If you supply the same
literal path to separate runs, you are explicitly sharing it; omit paths for
automatic isolation. No root directory, source checkout or user home serves as
an implicit workspace.

Explicit file mode starts with a minimal environment and replaces all inherited
home/app-data/temp aliases and `XDG_*` paths with private directories. Settings
cannot override those aliases or `ARVEL_PLUGIN_DATA_DIR`. The legacy invocation
without `--test-env` preserves environment inheritance, including env-selected
test stubs. Prefer explicit mode for isolated release tests.

The proxy probe removes inherited proxy variables and rejects proxy variables
or `SSH_AUTH_SOCK` in test/scenario env. It sends the unpredictable expected proxy
only in initialization, so a child must receive it through the plugin's actual
forwarding. Test overrides cannot supply the answer through startup env or replace
the reserved `conformance-canary` secret. Raw stdout/stderr are checked for canary
leakage before diagnostics are sanitized, including failed initialization and
failed scenarios.

## Placeholders and active probes

Static initialization/config/secrets/env values resolve before the plugin starts.
Supported placeholders are `{{tmpdir}}`, `{{dataDir}}`, `{{workspace}}` and
`{{fakevendor:name}}`. The default `tmpdir` is the run's workspace. Fake names are
single file stems, not paths. Directory path templates use the initially allocated
directory anchors; other initialization, env and step placeholders use the final
selected directories. Unknown, malformed and unavailable placeholders
fail configuration. Saved step variables such as `{{session}}` are available
only after the corresponding `save` step. String replacement preserves surrounding
JSON/YAML boolean, number, array and object types; it does not invoke a shell,
expand environment references or read credential files.

In explicit file mode, cancel/busy probes require observable streaming activity
from an unfinished turn. Configure a deterministic fake that emits an update and
then waits for cancellation. A completed turn or missing activity is a failure,
not evidence that the plugin lacks a capability. Concurrency tests issue both
requests together; a fixture barrier can prove actual overlap. Advertised resume
must accept load; unannounced resume is not required. Loading an existing live
session does not prove restoration after a process restart.

For compatibility, fast turns in the legacy no-file invocation can still report
cancel/busy SKIP. A missing proxy scenario also remains SKIP. Neither is a passed
assertion. The strict SDK fixture exercises all applicable provider probes with
PASS through real checker/plugin/fake executables.

## Go testkit APIs and lifetime

Existing `Start`, `StartDriver`, `InProcess`, `InProcessRouter`, `Initialize`,
`InitializeCtx`, `NewSession` and `LoadSession` signatures remain available.
Missing initialization metadata gets safe defaults; explicit metadata remains
unchanged. Use `Call` to send exact negative wire inputs such as a deliberately
missing data directory. `WithDir` continues to mean the process cwd.

`NewEnvironment(EnvironmentOptions{Initialize: meta, Workspace: cwd})` creates
missing paths and borrows explicit paths. Its `Initialize()` accessor returns a
cloned snapshot, `Workspace()` is the stable session directory, and `Root()` is
owned scratch space. `WithEnvironment(e)` borrows this context for a binary;
`InProcessWithEnvironment` and `InProcessRouterWithEnvironment` provide the same
choice in process. Close the host/driver before closing a borrowed environment.

For startup env, prepare a scenario before starting the binary:

```go
prepared, err := mockhost.PrepareScenario(ctx, sc, mockhost.PrepareOptions{
    FakevendorDir: "testdata/fakevendor",
})
if err != nil { return err }
defer prepared.Close()

env := procx.Env()
for name, value := range prepared.Env() { env.Set(name, value) }
env.Set("ARVEL_PLUGIN_DATA_DIR", prepared.Initialize().DataDir)
driver, err := mockhost.StartDriver(absoluteBinary,
    mockhost.WithEnvironment(prepared.Environment()),
    mockhost.WithDir(prepared.Initialize().DataDir),
    mockhost.WithEnv(env.Build()))
if err != nil { return err }
defer driver.Close() // executes before prepared.Close
result, runErr := prepared.Run(ctx, driver)
if runErr == nil && !result.Pass { runErr = errors.New("scenario assertions failed") }
closeErr := driver.CloseWithError()
return errors.Join(runErr, closeErr, prepared.Close())
```

Check `result.Pass` and step results as well as the returned error. This library
example uses a caller-selected minimal env; the CLI additionally applies the
private-home policy described above. `PrepareOptions.Environment` borrows an
existing context; conflicting scenario paths are rejected before startup.
`Tmpdir`, `FakevendorDir` and `Sandbox` are optional preparation inputs.
`PreparedScenario.Initialize()`/`Env()` return copies. `RunInitialized` executes
steps when a host has already sent initialization, for example after adding its
own controlled canary. Preparation itself never initializes or starts a plugin.

Existing `RunScenario` methods operate on an already-started driver or in-process
provider, so they cannot apply `plugin.env` at startup. Their signatures and
legacy env behavior remain; use `PrepareScenario` for the startup contract. Fakes
prepared by legacy calls now live until driver close.

`CloseWithError` reports teardown failures while `Close()` remains compatible.
Drivers join their transport/process workers before releasing an environment.
Closing an environment/preparation still in use fails without deleting files.
If termination is unconfirmed, resources are retained and the checker fails;
a timeout never authorizes deletion of potentially live data. Caller-owned
directories always survive. Raw capture accessors and `ScenarioResult.Updates`
may contain sensitive evidence: keep them out of reports and logs. Checker
diagnostics contain bounded check/field categories, not full RPC payloads,
resolved maps or subprocess stderr.
