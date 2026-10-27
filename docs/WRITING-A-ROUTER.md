# Writing a router plugin

A router suggests a task's type, role participation, a message's destination or
reasoning effort. It receives a restricted routing payload and has no provider
session interface. [PROTOCOL §14](PROTOCOL.md#14-router-plugins) is the wire
contract; [`examples/echo-router`](../examples/echo-router) is a deterministic,
local implementation with no credentials or network.

## Create a standalone router

From a directory containing an SDK clone, copy production files explicitly:

```sh
mkdir arvel-plugin-myrouter
cp arvel-sdk/examples/echo-router/main.go arvel-sdk/examples/echo-router/router.go arvel-sdk/examples/echo-router/rules.go arvel-plugin-myrouter/
cp arvel-sdk/examples/echo-router/arvel.json arvel-plugin-myrouter/
cp -R arvel-sdk/examples/echo-router/conformance arvel-plugin-myrouter/
cp arvel-sdk/templates/LICENSE arvel-plugin-myrouter/
cd arvel-plugin-myrouter
go mod init github.com/YOUR_ORG/arvel-plugin-myrouter
go get github.com/arvel-dev/arvel-sdk@v0.1.2
go mod tidy
go build -o bin/router .
go install github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check@v0.1.2
arvel-plugin-check --level release --manifest arvel.json --scenarios conformance -- ./bin/router
```

Use Go at least as new as the SDK's `go.mod`; build `bin/router.exe` on Windows.
Do not copy the SDK's router tests unchanged: they use its internal leak checker,
which Go forbids an external module from importing. An external-safe test is:

```go
package main

import (
    "testing"
    "github.com/arvel-dev/arvel-sdk/testkit/mockhost"
)

func TestPlanScenario(t *testing.T) {
    h := mockhost.InProcessRouter(t, router{})
    h.RunScenario(t, "conformance/plan.yaml")
    h.Shutdown()
}
```

## Interfaces and example rules

[`main.go`](../examples/echo-router/main.go) checks the error returned by
`plugin.RunRouter(ctx, router{}, opts...)`, prints failures only to stderr and
exits 1 on error. The required `plugin.Router` methods are Initialize, Status and
Shutdown. Return `plugin.RouterFeatures{}` from Initialize; capabilities are
inferred from optional interfaces:

| Interface | Method | Example behavior |
|---|---|---|
| `PlanRouter` | `Plan(ctx, protocol.RoutePlanParams)` | Match task type names/bug words and role descriptions |
| `MessageRouter` | `Message(ctx, protocol.RouteMessageParams)` | Pick a named candidate, otherwise keep current role with confidence 0 |
| `EffortRouter` | `Effort(ctx, protocol.RouteEffortParams)` | Map text length to a supplied effort level |
| `ExplainRouter` | `Explain(ctx, protocol.RoutePlanParams)` | Explain the deterministic rules that fired |

Implement at least one. Announce the same features in the manifest's
`capabilities.router.features`; do not invent sessions or provider capabilities.
`RouterHostAware.SetHost(plugin.RouterHost)` provides a JSON/redacting logger and
status notification handle before Initialize.

[`router.go`](../examples/echo-router/router.go) implements the interfaces;
[`rules.go`](../examples/echo-router/rules.go) contains the pure keyword rules.
Plan routing includes `optional`/`off` slots when the task names them, excludes
an `on` role preceded by "without"/"без", and never decides `auto` or
`lead_accept`. The built-in roster and order belong to the host. Behavior must
use IDs, slot states, kinds and resolved settings; display names are only input
text for a keyword rule and do not define privileges.

## Request-bound decisions and deadlines

Return only a type ID, slot, candidate role ID or effort value supplied in the
request. Plugin slots may be namespaced, for example `team-sample:docs-writer`;
do not hardcode the built-in roster. Confidence must be finite and in `[0,1]`.
The protocol result `Validate(request)` methods check these relationships;
RunRouter validates before sending, returning an internal error for invalid
results. Ignored decisions on `auto` and a type's `lead_accept` are dropped and
logged. The host chooses how to apply confidence and defaults.

Initialize is fast and offline. Route methods receive a context with a default
2.5-second framework timeout, below the host/checker's 3-second deadline. Remote
requests and any retries must honor that context and fit the remaining budget.
Use typed `protocol.NewError` failures, avoid background work outliving a request,
and let the host fall back to its configured task-type defaults on failure.

## Privacy

The host sends **only**: task title and description, the message text (for `route.message`), project names, the UI language, the task types (id, name, description, default roles) and for each role: `roleId`, name, description ("when this role is needed", ≤ 300 characters) and kind. **Never** code, diffs, file names, file contents, feed history, knowledge files, task codes from trackers or secrets of other plugins. The router MUST NOT ask for more and MUST NOT store request text beyond the request (debug logs follow §10).

This is the contract in PROTOCOL §14.2. A remote router must disclose its service
and retention rules. Never read workspace files or send another plugin's
credentials. Declare remote network hosts in the manifest; routers do not have
a provider sandbox block. Keep logs on stderr, redacted and appropriately gated
by `ARVEL_LOG_LEVEL`; debug text follows the protocol's truncation rule.

## Conformance and packaging

[`conformance/plan.yaml`](../examples/echo-router/conformance/plan.yaml) verifies
status, rejection of session/new and route results. Basic also checks empty
agentCapabilities, router-only features, shutdown and canary-secret leakage.
Release runs scenarios and 100 seeded requests with varied task types, built-in
and plugin slots, validating announced features under a 3-second deadline. Add a
fake-service stall fixture when routing calls a remote service.

Use the [router README](../templates/README.router.md) and binary CI/release
templates. Start with the **router example's manifest**, rather than the generic
provider manifest; replace six platform asset/bin entries, project_name and
binary consistently. Copy both template scripts, LICENSE and CHANGELOG; ignore
generated bin/dist/.release directories. No CGO is needed in release builds;
race tests require a C compiler. Pin the SDK/checker and review draft assets,
notes and checksums before publishing. Catalog topics are `arvel-plugin` and
`arvel-router`.
