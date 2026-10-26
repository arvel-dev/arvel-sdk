---
id: spec-arvel-provider-protocol
type: spec
status: accepted
---

# Arvel Provider Protocol — v1

This document is the contract between **Arvel host** (`arvel-host`, the Go core that runs on the user's computer or on their server) and its plugins: **provider plugins** (Claude, ChatGPT, OpenCode, and third-party ones), which run agents; **router plugins** (§14), which only help decide a task's type and roles; and **declarative plugins** (§16) that add roles, task types, MCP servers or skills. It is identical in every Arvel repository. If two copies disagree, the copy in `docs/PROTOCOL.md` of the `arvel-sdk` repository wins.

Words **MUST**, **SHOULD**, **MAY** have their RFC 2119 meaning.

---

## 1. Design goals

1. **One contract for every provider.** The host never contains provider-specific code. Everything that differs between Claude, Codex and OpenCode lives inside a plugin.
2. **Standards first.** The wire protocol is the [Agent Client Protocol (ACP)](https://agentclientprotocol.com) — plugin = ACP *agent*, host = ACP *client*. Arvel-only data travels in ACP's own extension points: `_meta` objects and `_`-prefixed methods. A generic ACP agent therefore works with Arvel with reduced features, and an Arvel plugin works in any ACP client.
3. **Official vendor surfaces only.** A plugin drives the vendor's own CLI through the vendor's documented programmatic interface. A plugin never reads, copies or replays another program's credentials or OAuth tokens.
4. **Host decides, plugin enforces.** The host owns policy (roles, permissions, limits, budgets). The plugin translates that policy into the vendor CLI's native controls *and* routes every remaining approval back to the host.
5. **Fail loud and typed.** Every failure the host can act on (rate limit, missing login, missing binary) has a stable machine-readable code.

---

## 2. Process model and transport

| Item | Rule |
|---|---|
| Process | One provider plugin process **per connection and workspace** (see §4 and §15: the process is the sandbox boundary, so two workspaces never share one). One router plugin process per connection. `team` plugins have no process; an `integration` plugin's MCP server runs as an MCP server, not as a plugin process (§16.4). The host starts a plugin process, keeps it alive, restarts it on crash with exponential backoff (1 s → 30 s, reset after 5 min healthy). One process hosts many ACP sessions. |
| Transport | stdio. **stdin/stdout:** ACP JSON-RPC 2.0, one JSON object per line (`\n`-delimited, UTF-8, no embedded raw newlines). **stderr:** logs only (§10). |
| Max line | Both sides MUST accept lines up to 64 MiB. |
| Working directory | Host starts the plugin with cwd = plugin data dir (§3.3). Sessions carry their own `cwd`. |
| Environment | Host passes a **minimal** environment: `PATH`, `HOME`/`USERPROFILE`, `LANG`, `TMPDIR`/`TEMP`, platform essentials, plus `ARVEL_PLUGIN_DATA_DIR`, `ARVEL_LOG_LEVEL`. Secrets are **never** passed through env to the plugin; they arrive in `initialize` (§5.1). The plugin decides what env the vendor CLI gets. |
| Shutdown | Host sends `_arvel/shutdown` (notification), waits 5 s, closes stdin, waits 5 s, then kills the process tree. Plugin MUST cancel running turns, ask vendor CLIs to finish (interrupt), and exit 0. |
| Platforms | Plugins MUST support `darwin/arm64`, `darwin/amd64`, `linux/amd64`, `linux/arm64`, `windows/amd64`, `windows/arm64`. |

---

## 3. Packaging

### 3.1 Repository and discovery

- GitHub repository named `arvel-plugin-<id>` (recommended, not required).
- Topics: `arvel-plugin` (required) + exactly one of `arvel-provider`, `arvel-router`, `arvel-team`, `arvel-integration` (matching `type`).
- `arvel.json` in the repository root **and** attached to every release.
- Releases are tagged `vMAJOR.MINOR.PATCH` (semver). The host installs only tagged releases, never branches.

### 3.2 `arvel.json` manifest

```json
{
  "$schema": "https://raw.githubusercontent.com/arvel-dev/arvel-sdk/main/schema/arvel.schema.json",
  "id": "claude",
  "name": "Claude",
  "type": "provider",
  "version": "0.1.0",
  "pluginApi": "1",
  "description": "Claude models through the official claude CLI — with your Claude plan or an API key.",
  "homepage": "https://github.com/arvel-dev/arvel-plugin-claude",
  "license": "Apache-2.0",
  "entry": {
    "darwin-arm64":  { "asset": "arvel-plugin-claude_darwin_arm64.tar.gz",  "bin": "arvel-plugin-claude" },
    "darwin-amd64":  { "asset": "arvel-plugin-claude_darwin_amd64.tar.gz",  "bin": "arvel-plugin-claude" },
    "linux-amd64":   { "asset": "arvel-plugin-claude_linux_amd64.tar.gz",   "bin": "arvel-plugin-claude" },
    "linux-arm64":   { "asset": "arvel-plugin-claude_linux_arm64.tar.gz",   "bin": "arvel-plugin-claude" },
    "windows-amd64": { "asset": "arvel-plugin-claude_windows_amd64.zip",    "bin": "arvel-plugin-claude.exe" },
    "windows-arm64": { "asset": "arvel-plugin-claude_windows_arm64.zip",    "bin": "arvel-plugin-claude.exe" }
  },
  "checksums": "checksums.txt",
  "requires": [
    {
      "binary": "claude",
      "minVersion": "2.1.259",
      "versionArgs": ["--version"],
      "install": {
        "darwin": "curl -fsSL https://claude.ai/install.sh | bash",
        "linux": "curl -fsSL https://claude.ai/install.sh | bash",
        "windows": "irm https://claude.ai/install.ps1 | iex",
        "docs": "https://code.claude.com/docs/en/setup"
      }
    }
  ],
  "permissions": {
    "spawn": ["claude"],
    "network": ["api.anthropic.com"],
    "files": "session-cwd"
  },
  "sandbox": {
    "vendorPaths": { "read": ["~/.claude", "~/.claude.json"], "write": ["~/.claude", "~/.claude.json"] },
    "vendorHosts": ["api.anthropic.com", "*.anthropic.com", "claude.ai"],
    "macosKeychain": true
  },
  "connections": [ /* §4 */ ],
  "capabilities": {
    "usage": "windows+spend",
    "subagents": true,
    "resume": true,
    "modelSwitch": "live",
    "effortSwitch": "next-turn"
  }
}
```

Field rules:

| Field | Rule |
|---|---|
| `id` | `^[a-z][a-z0-9-]{1,39}$`, globally unique in the catalog, never changes. |
| `type` | `provider` (§4–§10), `router` (§14), `team` (§16: roles and task types, no process) or `integration` (§16: MCP servers and skills for agents, optionally roles and task types). |
| `pluginApi` | Major version of this protocol as a string: `"1"`. Host refuses other majors. |
| `entry` | Map `os-arch` → release asset + binary path inside the archive. Missing platform = not installable there; the host shows that. |
| `checksums` | Release asset with `sha256  filename` lines. Host MUST verify before unpacking. |
| `requires[]` | External programs the plugin drives. The host shows install hints; the plugin itself re-checks at runtime and reports through `_arvel/status`. |
| `permissions` | What the plugin may do. `spawn`: binary names it may execute. `network`: hosts it contacts *directly* (not hosts the vendor CLI contacts). `files`: `session-cwd` (default) — only the session cwd and its own data dir. The host shows these on install and shows a **diff** when an update asks for more. |
| `sandbox` | Providers only. What the vendor CLI needs **outside** the workspace to work at all (§15): its own state paths, the hosts of its own API, macOS keychain access for a stored sign-in. Shown on install and in the update diff. Forbidden in `vendorPaths`: `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.kube`, `~/.config/gcloud`, `~/.docker`, browser profiles, `/` and `~` themselves (manifest validation error). |
| `connections[]` | See §4. At least one. |
| `capabilities` | Static hints for the UI. The runtime truth is the `initialize` response (§5.1). |

**Command entry (generic ACP agents).** A plugin may have no binary of its own. It can instead point at an ACP agent that the user installs separately:

```json
"entry": { "command": ["gemini", "--experimental-acp"] }
```

The host then speaks plain ACP to that command. It must not assume any `_arvel/*` support:
- status comes from the `requires[]` checks;
- models and effort come from ACP `configOptions` (categories `model`, `thought_level`);
- usage is `unknown`;
- permissions still go through `session/request_permission`.

These plugins are labelled "basic ACP" in the catalog.

The JSON Schema for `arvel.json` lives in `schema/arvel.schema.json` of the `arvel-sdk` repository.

### 3.3 Install layout (host side, informative)

```
<arvel data>/plugins/<id>/<version>/        ← unpacked release (read-only)
<arvel data>/plugins/<id>/data/<connection>/ ← ARVEL_PLUGIN_DATA_DIR (read-write, survives updates)
```

`<arvel data>` is `~/Library/Application Support/Arvel` (macOS), `%APPDATA%\Arvel` (Windows), `$XDG_DATA_HOME/arvel` (Linux). Removing a plugin deletes both. Arvel ships **no built-in plugins**: every provider is installed from the catalog (first run offers the ones whose CLI is already on the computer first) or from a release archive file.

---

## 4. Connections

A **connection** is one way to reach a provider: one account, one billing mode. A plugin declares connection *kinds*; the user creates connection *instances* in Arvel. Each instance gets its own plugin process and data dir.

```json
"connections": [
  {
    "kind": "subscription",
    "label": "Claude plan (via claude CLI)",
    "billing": "subscription",
    "description": "Uses the claude CLI signed in with your own Claude account.",
    "config": {
      "type": "object",
      "properties": {
        "loadProjectSettings": { "type": "boolean", "default": false, "title": "Load the project's own Claude settings (hooks, .mcp.json)" }
      }
    },
    "secrets": []
  },
  {
    "kind": "api_key",
    "label": "Claude API key",
    "billing": "api",
    "config": { "type": "object", "properties": {} },
    "secrets": [
      { "id": "apiKey", "label": "Anthropic API key", "pattern": "^sk-ant-", "required": true }
    ]
  }
]
```

- `billing`: `subscription` | `api` | `mixed` | `unknown`. Drives the UI: `$` caps are shown only for `api`/`mixed`; window percentages for `subscription`/`mixed`.
- `config`: a **JSON Schema subset** the host renders as a form: `type: object` with `properties` of `boolean`, `string` (optional `enum`, `pattern`), `integer` (`minimum`/`maximum`). `title` and `description` are UI text. No nesting.
- `secrets`: values the host stores in the OS keychain and passes only in `initialize`. The plugin MUST NOT write secrets to disk or logs.

---

## 5. Lifecycle

### 5.1 `initialize` (ACP)

Host → plugin. Standard ACP fields plus `_meta.arvel`:

```json
{
  "jsonrpc": "2.0", "id": 1, "method": "initialize",
  "params": {
    "protocolVersion": 1,
    "clientCapabilities": { "fs": { "readTextFile": false, "writeTextFile": false }, "terminal": false },
    "clientInfo": { "name": "arvel", "version": "1.0.0" },
    "_meta": {
      "arvel": {
        "pluginApi": "1",
        "connection": {
          "id": "c_01J...",
          "kind": "subscription",
          "config": { "loadProjectSettings": false },
          "secrets": {}
        },
        "dataDir": "/Users/me/Library/Application Support/Arvel/plugins/claude/data/c_01J...",
        "locale": "ru-RU",
        "platform": { "os": "darwin", "arch": "arm64", "remote": false },
        "sandbox": { "mode": "os", "proxy": "http://127.0.0.1:41234", "workspace": "w_01J…" }
      }
    }
  }
}
```

- `clientCapabilities.fs/terminal` are **false**: vendor CLIs use their own file and shell tools inside the session cwd; Arvel controls them through permissions (§7), not by proxying I/O.
- `sandbox` (§15): `mode` is `os`, `container` or `none`; `proxy` is the only address the process may connect to for the internet. The plugin MUST pass it to the vendor CLI as `HTTPS_PROXY`, `HTTP_PROXY` and `ALL_PROXY`, with `NO_PROXY=localhost,127.0.0.1`.
- `platform.remote` is `true` when the host runs on a server reached over SSH. Plugins use it to prefer device-code / copy-paste login flows (§6.4).

Plugin → host response:

```json
{
  "jsonrpc": "2.0", "id": 1,
  "result": {
    "protocolVersion": 1,
    "agentCapabilities": {
      "loadSession": true,
      "promptCapabilities": { "image": true, "audio": false, "embeddedContext": true },
      "mcpCapabilities": { "http": true, "sse": false }
    },
    "agentInfo": { "name": "arvel-plugin-claude", "version": "0.1.0" },
    "authMethods": [],
    "_meta": {
      "arvel": {
        "pluginApi": "1",
        "features": ["status", "models", "usage", "actions", "subagents", "resume", "skills", "discover"],
        "instructionFiles": ["CLAUDE.md"],
        "skills": { "projectDirs": [".claude/skills"], "spec": "agentskills/1" }
      }
    }
  }
}
```

`initialize` MUST answer within 5 s and MUST NOT block on network or on the vendor CLI. Slow checks happen in `_arvel/status`.

The optional fields in the response:

- `instructionFiles` — files the vendor CLI reads automatically from the working directory, such as `CLAUDE.md` or `AGENTS.md`. The host composes project instructions into these files inside the task worktree and hides them from git.
- `skills.projectDirs` — where the vendor CLI looks for project skills, relative to the session cwd.
- `skills.spec` — the skill format the CLI understands; `agentskills/1` means the open Agent Skills `SKILL.md` format.
- `discover` in `features` — the plugin supports §7.9.

### 5.2 Readiness

Right after `initialize` the host calls `_arvel/status`. Sessions are created only when `state == "ready"`. The plugin sends `_arvel/status_changed` whenever the state changes (e.g. the user signed in from a terminal).

---

## 6. Arvel extension methods

All are JSON-RPC methods or notifications with a `_arvel/` prefix. Unknown `_arvel/*` methods MUST return error `-32601`.

### 6.1 `_arvel/status` (request)

```json
// result
{
  "state": "ready",                       // ready | not_installed | outdated | needs_login | degraded | error
  "message": "Signed in · Max plan",      // short, localized when possible (uses initialize locale)
  "binary": { "name": "claude", "path": "/Users/me/.local/bin/claude", "version": "2.1.310" },
  "account": { "label": "me@example.com", "plan": "max" },  // label may be masked; omit if unknown
  "billingNote": "Runs started by Arvel count as programmatic use of your plan.", // optional
  "actions": [ { "id": "login", "label": "Sign in", "kind": "terminal" } ]
}
```

`state` semantics:

| state | Meaning | Host behaviour |
|---|---|---|
| `ready` | Can run sessions. | Normal. |
| `not_installed` | Required binary not found. | Shows install hint from manifest + `actions`. |
| `outdated` | Binary below `minVersion`. | Shows update hint. |
| `needs_login` | Binary present, not authenticated for this connection kind. | Shows `actions` (login). |
| `degraded` | Works with reduced features (e.g. usage unavailable). | Normal + warning. |
| `error` | Anything else. `message` explains. | Connection unusable. |

### 6.2 `_arvel/models` (request)

```json
// params
{ "refresh": false }
// result
{
  "models": [
    {
      "id": "claude-sonnet-5-5",                 // value passed back in session/new
      "label": "Sonnet 5.5",
      "description": "Fast, strong default for coding",
      "aliases": ["sonnet"],
      "efforts": ["low", "medium", "high", "xhigh", "max"],   // [] = model cannot reason/effort is fixed
      "defaultEffort": "medium",
      "contextWindow": 1000000,                  // optional
      "inputs": ["text", "image"],
      "billing": "subscription",                 // per this connection
      "recommendedFor": ["implement"],           // optional hint: plan | implement | review | test | background
      "hidden": false
    }
  ],
  "default": "claude-sonnet-5-5"
}
```

Effort vocabulary is fixed: `low`, `medium`, `high`, `xhigh`, `max`. The host's UI level **Auto** means "send no effort" (provider default / router decides). Plugins map this vocabulary to the vendor's own values and omit levels the model lacks.

### 6.3 `_arvel/usage` (request) and `_arvel/usage_updated` (notification)

```json
{
  "source": "exact",                  // exact | estimated | unknown
  "status": "ok",                     // ok | warning | exhausted
  "windows": [
    { "id": "five_hour", "label": "5-hour window", "usedPercent": 38, "resetsAt": "2026-10-25T18:40:00Z", "durationMinutes": 300 },
    { "id": "weekly",    "label": "Week",          "usedPercent": 61, "resetsAt": "2026-10-26T09:00:00Z", "durationMinutes": 10080 }
  ],
  "spend": { "currency": "USD", "sinceStart": 1.84, "source": "estimated" },
  "updatedAt": "2026-10-25T16:48:00Z"
}
```

- `windows` for subscription-like quotas, `spend` for API billing. Either may be empty.
- `status = warning` when any window ≥ 80 % or the vendor says so; `exhausted` when the vendor rejects requests.
- Plugins SHOULD push `_arvel/usage_updated` (same body) whenever the vendor reports new numbers, at most once per 5 s.
- When the plugin cannot know limits, it returns `source: "unknown"` and the host learns about limits only from typed errors (§9).

### 6.4 `_arvel/action` (request)

Runs one of the `actions` from `_arvel/status`.

```json
// params
{ "id": "login" }
// result — one of:
{ "kind": "terminal", "command": ["claude", "auth", "login"], "env": {}, "explain": "Finish sign-in in this terminal." }
{ "kind": "url", "url": "https://auth.openai.com/...", "explain": "Open this link and sign in." , "awaitCompletion": true }
{ "kind": "done", "message": "Signed out." }
```

- `terminal`: the host opens its built-in terminal **on the machine where the host runs** (local or server) and runs the command. The plugin never sees what the user types. After the terminal closes the host calls `_arvel/status`.
- `url`: the host opens the URL in the user's browser. With `awaitCompletion` the plugin later sends `_arvel/status_changed`.
- When `platform.remote` is true, plugins MUST prefer flows that work without a browser on that machine (device code, paste-back code).

### 6.5 `_arvel/status_changed` (notification, plugin → host)

Body = `_arvel/status` result.

### 6.6 `_arvel/shutdown` (notification, host → plugin)

No params. See §2.

---

## 7. Sessions

Standard ACP methods: `session/new`, `session/load`, `session/prompt`, `session/cancel`, `session/set_config_option`, notifications `session/update`, requests `session/request_permission`.

### 7.1 `session/new` with `_meta.arvel`

```json
{
  "method": "session/new",
  "params": {
    "cwd": "/srv/arvel/worktrees/shop/task-42",
    "mcpServers": [
      { "name": "arvel", "command": "/opt/arvel/arvel-host", "args": ["mcp", "--session", "s_01J..."], "env": [] }
    ],
    "_meta": {
      "arvel": {
        "role": { "id": "developer", "name": "Developer" },
        "task": { "number": 42, "code": "SHOP-1287", "type": "feature", "readOnly": false },
        "model": "claude-sonnet-5-5",
        "effort": "medium",                 // omitted = Auto
        "instructions": "…role instructions + project rules, already merged by the host…",
        "language": "ru",                   // language for replies to the user
        "permissions": {
          "read":    "allow",               // allow | ask | deny
          "edit":    "allow",
          "commands": { "default": "ask", "allow": ["go test *", "go vet *"], "deny": ["git push *"] },
          "network": "ask",
          "mcp":     { "default": "ask", "tools": { "postgres/query": "allow", "postgres/execute": "deny" } }
        },
        "subagents": { "enabled": true, "model": "claude-haiku-5-5", "effort": "low", "maxParallel": 2 },
        "budget": { "maxTurns": 200, "maxUsd": 5.0 },   // maxUsd only meaningful for api billing; may be absent
        "skills": [                          // Agent Skills folders the host prepared for this session (read-only)
          { "name": "laravel-migrations", "path": "/…/arvel/skills/laravel-migrations", "invocation": "auto" },
          { "name": "release-notes", "path": "/…/arvel/skills/release-notes", "invocation": "manual" }
        ],
        "resumeFrom": null                  // or a vendor session id for continuing elsewhere
      }
    }
  }
}
```

`role.id` is one of the built-in ids `lead`, `architect`, `developer`, `reviewer`, `security`, `tester`, `keeper`, or `<pluginId>:<roleId>` for a role added by a plugin (§16). `task.type` is a built-in type id (`spec`, `feature`, `bug`, `edit`, `review`, `analysis`) or `<pluginId>:<typeId>`. Both are informative (logs, vendor session titles): plugins MUST NOT change behaviour by role or type. `task.readOnly: true` means the session works on a read-only view of a branch; the host also sends `permissions.edit: "deny"`, and the plugin MUST map it so that no file in `cwd` can be changed.

Plugin obligations:

1. **cwd is the boundary.** Configure the vendor CLI so file edits outside `cwd` are denied or asked.
2. **Baseline mapping.** Translate `permissions` into the vendor CLI's native controls (allow/deny lists, sandbox, approval policy) so that `allow` needs no round-trip and `deny` never reaches the host. Everything else MUST arrive at the host as `session/request_permission` (§7.3). The plugin MUST NOT silently approve anything the policy marks `ask`.
3. **Never bypass.** Plugins MUST NOT use vendor "skip all permissions" modes.
4. **Instructions** are appended to the vendor's default system prompt (never replace it).
5. **MCP servers** from `mcpServers` MUST be made available to the vendor CLI, and *only* those plus the vendor's built-ins (project-defined MCP servers are off unless the connection config enables them).
5a. **Skills** from `_meta.arvel.skills` MUST be made visible to the vendor CLI in its native way. Examples: copy them into a `skills.projectDirs` folder inside `cwd`, excluded from git through the worktree's `info/exclude`; or pass an extra directory flag. The plugin MUST NOT modify the source folders, and MUST NOT overwrite a skill of the same name that the repository already has; it prefixes its copy with `arvel-` instead. `invocation: "manual"` maps to the vendor's "do not auto-invoke" switch where one exists. The plugin removes its copies when the session closes.
6. **Budget**: pass `maxTurns` / `maxUsd` to the vendor when it supports them; otherwise enforce in the plugin and stop with `budget_exceeded` (§9).

Response: `{ "sessionId": "…", "configOptions": [...] }`. The plugin SHOULD return ACP config options with categories `model` and `thought_level` so generic ACP clients can switch them.

### 7.2 Prompt turns

`session/prompt` carries ACP content blocks (`text`, `image`, `resource_link`, `resource`). The plugin streams `session/update` notifications and finally answers the request with `{ "stopReason": "end_turn" | "max_tokens" | "max_turn_requests" | "refusal" | "cancelled" }` plus optional `_meta.arvel.stop` (§9) and `_meta.arvel.usage`:

```json
"_meta": { "arvel": { "usage": { "inputTokens": 120334, "outputTokens": 8812, "cacheReadTokens": 98000, "costUsd": 0.42, "costSource": "estimated" } } }
```

Mapping of vendor events to ACP `session/update`:

| Vendor event | ACP `sessionUpdate` |
|---|---|
| Assistant text delta | `agent_message_chunk` |
| Reasoning / thinking delta | `agent_thought_chunk` |
| Tool call started | `tool_call` (`kind`: `read` `edit` `delete` `move` `search` `execute` `think` `fetch` `other`; `locations` with paths) |
| Tool progress / result | `tool_call_update` (`status`: `pending` `in_progress` `completed` `failed`; `content` with diff or text) |
| Plan / todo list | `plan` |
| Model or effort changed by vendor (e.g. fallback) | `config_option_update` |

Subagents: every update produced inside a subagent carries `_meta.arvel.subagent = { "id": "…", "parentToolCallId": "…", "label": "Explore" }`. The host shows them as a collapsible group and does **not** add their text to the parent context.

### 7.3 Permission requests

```json
{
  "method": "session/request_permission",
  "params": {
    "sessionId": "…",
    "toolCall": { "toolCallId": "t_9", "title": "Run go test ./...", "kind": "execute", "rawInput": { "command": "go test ./..." } },
    "options": [
      { "optionId": "allow_once",   "name": "Allow",            "kind": "allow_once" },
      { "optionId": "allow_always", "name": "Allow for session", "kind": "allow_always" },
      { "optionId": "reject_once",  "name": "Deny",             "kind": "reject_once" }
    ],
    "_meta": {
      "arvel": {
        "category": "command",            // command | edit | read | network | mcp | other
        "command": "go test ./...",
        "paths": [],
        "hosts": [],
        "reason": "Not in the role's allow list"
      }
    }
  }
}
```

- `allow_always` means **for this session only**. Persistent rules are the host's business; plugins MUST NOT persist approvals into vendor config files.
- The host may answer minutes or hours later (the user approves from a Telegram bot). Plugins MUST NOT time out permission requests; they wait until answered or the session is cancelled.
- On `session/cancel` with pending permission requests, the plugin answers them as rejected towards the vendor and ends the turn with `cancelled`.

### 7.4 `session/load` (resume)

`session/load { sessionId, cwd, mcpServers, _meta.arvel }` resumes a vendor session (same model unless overridden). The plugin replays history as `session/update` notifications only if the vendor exposes it; otherwise it resumes silently and the host uses its own stored feed.

### 7.5 Switching model / effort

`session/set_config_option` with `configId` `model` or `effort`. Applies **from the next turn**. If the vendor can switch live, do it; else restart the vendor process with resume at the next turn boundary. Never interrupt a running turn for this.

### 7.6 Cancellation

`session/cancel` (notification) → plugin interrupts the vendor turn using the vendor's *graceful* mechanism, waits up to 10 s, then kills the vendor process tree (and will resume on next prompt). The pending `session/prompt` resolves with `stopReason: "cancelled"`.

### 7.7 Skills and MCP are the host's catalogue

The host owns the catalogue of skills and MCP servers, along with their scopes and the per-role assignment. Plugins only *deliver* what arrives in a session. They never install skills or MCP servers themselves, and with default connection settings they never load the user's personal skill or MCP configuration into Arvel sessions. The only exception is **read-only discovery on the user's request** (§7.9), which lets the host offer to copy existing definitions into its catalogue.

### 7.8 Reserved

Interactive terminal sessions were dropped before v1 was released: the user talks to agents only through the task feed. The method name `_arvel/interactive` and the feature name `interactive` are reserved and MUST NOT be used.

---

### 7.9 Discovery of existing skills and MCP servers (optional feature `discover`)

The host knows nothing about where a vendor CLI keeps its configuration. A provider plugin that declares `discover` in `features` answers:

```json
{ "method": "_arvel/discover",
  "params": { "kinds": ["mcp", "skills"], "projects": ["/Users/me/code/shop-api", "/Users/me/code/shop-admin"] } }
```

```json
{ "result": {
    "mcpServers": [
      { "name": "postgres", "scope": "project", "project": "/Users/me/code/shop-api", "source": ".mcp.json",
        "transport": "stdio", "command": ["npx", "-y", "@modelcontextprotocol/server-postgres@0.6.1"],
        "env": { "DATABASE_URL": { "secret": true } } },
      { "name": "github", "scope": "user", "source": "~/.claude.json", "transport": "http", "url": "https://mcp.github.example/v1",
        "headers": { "Authorization": { "secret": true } } }
    ],
    "skills": [
      { "name": "pest-tests", "scope": "user", "source": "~/.claude/skills/pest-tests", "path": "/Users/me/.claude/skills/pest-tests" }
    ]
} }
```

Rules:
- Called only when the user presses an import action in the UI; never on a schedule.
- **Read-only.** The plugin reads only the vendor's documented configuration and skill locations (its SPEC lists them), never credential files (§11.1).
- **Secret values never leave the plugin.** Env vars and headers are returned by name with `{ "secret": true }` when the value is a literal or looks like a key; a reference to an env var is returned as `{ "envRef": "NAME" }`. The host asks the user to enter secrets into its own store.
- `source` is a display string for the UI; `path` is an absolute path the host may read to copy a skill folder (inside the listed projects or the user's home only).
- Unknown fields are ignored; an empty result is valid.

## 8. Concurrency

- Many sessions per plugin process; at most one turn per session at a time (`session/prompt` while one runs → error `-32002` with `arvel.code = "busy"`).
- Plugins SHOULD run one vendor process per active session and stop it after 15 min idle (resume later).
- The host limits concurrency per connection; plugins MAY expose a soft limit in `_meta.arvel.features` as `maxParallelSessions:N`.

---

## 9. Errors and stop details

JSON-RPC errors use ACP codes; Arvel detail sits in `error.data.arvel`. Turn-level outcomes that are not protocol errors go in the `session/prompt` result as `_meta.arvel.stop`.

```json
{ "code": "rate_limited", "retryable": true, "resetsAt": "2026-10-25T18:40:00Z", "windowId": "five_hour", "message": "5-hour window exhausted" }
```

| `code` | When | Required fields |
|---|---|---|
| `rate_limited` | Vendor quota/window exhausted or 429 that will not clear by retrying. | `resetsAt` if known, `windowId` if known |
| `auth_required` | Not signed in / key invalid / token expired. | — |
| `binary_missing` | Vendor CLI vanished. | — |
| `binary_outdated` | Vendor CLI below minimum. | `minVersion`, `foundVersion` |
| `model_unavailable` | Model not offered for this account. | `model` |
| `budget_exceeded` | `maxTurns` or `maxUsd` reached. | `limit`: `turns`/`usd` |
| `context_overflow` | Context could not be compacted. | — |
| `overloaded` | Vendor overloaded after retries. | `retryAfterSeconds` if known |
| `busy` | Session already has a running turn. | — |
| `provider_error` | Anything else from the vendor. | `vendorCode`, `message` |
| `internal` | Plugin bug. | `message` |

The host's limit policy (wait for reset, switch to a fallback executor, ask the user) relies on `rate_limited` and `resetsAt` — plugins MUST detect quota errors precisely and MUST NOT report ordinary transient errors as `rate_limited`.

When a turn stops for `rate_limited`, the plugin MUST leave the vendor session resumable so the host can continue it after reset with `session/load` + a new prompt.

---

## 10. Logging

- stderr, one JSON object per line: `{"ts":"…","level":"info","msg":"…","session":"…","plugin":"claude", …}` (Go `log/slog` JSON handler).
- Level from `ARVEL_LOG_LEVEL` (`debug|info|warn|error`, default `info`).
- MUST NOT log secrets, full prompts or file contents at `info` and above. At `debug` prompts may be logged truncated to 2 KiB.
- Vendor CLI stderr is forwarded at `debug` with `"source":"vendor"`.

---

## 11. Security rules (summary)

1. No credential scraping: never read `~/.claude*`, `~/.codex/auth.json`, OpenCode auth files, keychains or browser data. Ask the vendor CLI (`auth status`, `account/read`, …) instead.
2. Secrets only from `initialize`; keep them in memory; pass to vendor processes via env or stdin only when required; scrub them from any error text.
3. Spawn only binaries listed in `permissions.spawn`; resolve full paths at startup; never use a shell (`sh -c`, `cmd /c`) to start them — except Windows `.cmd`/`.bat` shims, which must be launched with proper argument quoting.
4. Strip variables that change vendor auth from the vendor env unless the connection sets them deliberately (e.g. remove `ANTHROPIC_API_KEY` for a Claude *subscription* connection, remove `OPENAI_API_KEY` for a ChatGPT connection).
5. Kill whole process trees (process groups on Unix, Job Objects on Windows).
6. Never weaken or escape the host's sandbox (§15): no attempts to reach the network around the proxy, no requests for paths outside the sandbox, and never turn off the vendor CLI's own sandbox unless FINDINGS shows it cannot run nested inside the host's sandbox on that OS.

---

## 12. Versioning

- `pluginApi` major `"1"`. Additions within v1 are backward compatible: new optional fields, new `_arvel/*` methods, new error codes. Unknown fields MUST be ignored by both sides.
- Removing or changing meaning of a field = v2.

## 13. Conformance

`arvel-plugin-check` (in the Arvel SDK, `arvel-sdk/cmd/arvel-plugin-check`) runs a plugin against a scripted mock host and verifies §2–§10 (§14 for routers, §16.5 for `team` and `integration` plugins), including that the proxy from `sandbox` reaches the vendor CLI's environment (§5.1). A plugin release MUST pass `arvel-plugin-check --level=release` in CI.

---

## 14. Router plugins (`type: "router"`)

A router plugin helps the host decide **which task type fits a new task and which roles it needs, which role answers a message, and which reasoning effort to use when a role is set to Auto**. It never runs an agent, never sees code or files, and never decides anything the user's settings forbid.

Without a router plugin the host uses its built-in **type router** (§14.6): the user picks the type, the type ticks its roles. A router is enabled per workspace on the router's own settings page («Использовать в пространстве»); at most one router is active per workspace.

### 14.1 What stays the same

Everything in §2 (process, transport, shutdown), §3 (packaging; `type: "router"`, topic `arvel-router`), §4 (connections: a router declares at least one connection kind; its `config` becomes the router's settings form and its `secrets` the keys), §5 (`initialize`, `_arvel/status`), §9 (errors), §10 (logging), §11 (security) and §12 (versioning).

Differences:
- `initialize` result: `agentCapabilities` is `{}` and `_meta.arvel.features` lists only router features: `route.plan`, `route.message`, `route.effort` (any subset, at least one), optionally `route.explain`.
- A router MUST NOT implement `session/*` methods; the host never calls them (`-32601` if it does).
- `capabilities` in the manifest: `{ "router": { "features": [...], "maxLatencyMs": 3000 } }`.
- `permissions.network` MUST list every host the router contacts. `permissions.spawn` SHOULD be empty.
- The host runs a router sandboxed like a provider (§15) but with no file access beyond its data dir and with the proxy allowing only `permissions.network`.

### 14.2 What the host sends (privacy contract)

The host sends **only**: task title and description, the message text (for `route.message`), project names, the UI language, the task types (id, name, description, default roles) and for each role: `roleId`, name, description ("when this role is needed", ≤ 300 characters) and kind. **Never** code, diffs, file names, file contents, feed history, knowledge files, task codes from trackers or secrets of other plugins. The router MUST NOT ask for more and MUST NOT store request text beyond the request (debug logs follow §10).

Role kinds: `intake` (the lead's analysis at the start), `plan`, `code`, `check` (review, security, tests — may return findings), `accept` (the lead's acceptance at the end), `knowledge`.

### 14.3 `_arvel/route/plan` (request)

Called when the user opens New task (after a 600 ms pause in typing) and again on "Create" if the text changed.

```json
{ "jsonrpc": "2.0", "id": 7, "method": "_arvel/route/plan",
  "params": {
    "task": { "title": "Webhook signature test fails", "description": "After the last commit …", "projects": ["shop-api"], "language": "ru" },
    "types": [
      { "typeId": "feature", "name": "Feature", "description": "One change with code, the full role chain", "roles": { "lead_intake": "on", "architect": "on", "developer": "on", "reviewer": "on", "security": "auto", "tester": "on", "lead_accept": "on" } },
      { "typeId": "bug", "name": "Bug", "description": "Diagnosis and fix", "roles": { "developer": "on", "reviewer": "on", "security": "auto", "tester": "on", "lead_accept": "on" } },
      { "typeId": "analysis", "name": "Analysis", "description": "An answer without changes", "roles": { "lead_intake": "on", "architect": "optional" } }
    ],
    "roles": [
      { "slot": "lead_intake", "roleId": "lead",      "name": "Lead · intake",  "description": "Turns the request into requirements and acceptance criteria…", "kind": "intake" },
      { "slot": "architect",   "roleId": "architect", "name": "Architect",      "description": "Plans changes that touch several modules…", "kind": "plan" },
      { "slot": "developer",   "roleId": "developer", "name": "Developer",      "description": "Writes code by the plan…", "kind": "code" },
      { "slot": "reviewer",    "roleId": "reviewer",  "name": "Reviewer",       "description": "…", "kind": "check" },
      { "slot": "security",    "roleId": "security",  "name": "Security",       "description": "Checks auth, payments, secrets…", "kind": "check" },
      { "slot": "tester",      "roleId": "tester",    "name": "Tester",         "description": "Runs tests and browser checks…", "kind": "check" },
      { "slot": "lead_accept", "roleId": "lead",      "name": "Lead · acceptance", "description": "Checks the result, commits and pushes…", "kind": "accept" }
    ]
  } }
```

`slot` is a place in the fixed order (§14.6). A role added by a plugin has its own slot id `<pluginId>:<roleId>`. A type's `roles` map gives each slot's default: `on`, `off`, `auto` (the host includes it by a rule, e.g. Security when the change touches sensitive areas) or `optional` (off, offered as a suggestion); missing slots are `off`.

```json
{ "jsonrpc": "2.0", "id": 7, "result": {
    "type": { "typeId": "bug", "confidence": 0.84 },
    "roles": [
      { "slot": "lead_intake", "include": false, "confidence": 0.91 },
      { "slot": "tester",      "include": true,  "confidence": 0.71 }
    ]
} }
```

Rules (the host enforces them; `arvel-plugin-check` tests them):
- `type` is optional and is `null` when no type fits; `typeId` MUST be one of the given ids. If the user already chose a type by hand, the host ignores this field.
- `roles[]` decisions are relative to the chosen type's defaults; every `slot` MUST be one of the given slots; decisions on `auto` slots and on `lead_accept` of a type that has it are ignored and logged.
- `confidence` is in `[0, 1]`. The host applies a decision only when `confidence` ≥ the user's threshold; below it the role keeps its default and the UI shows the suggestion as a question.
- The router cannot reorder roles, add roles that are not given, change what happens after a role, or change branches.

### 14.4 `_arvel/route/message` (request)

Called for a message in a running task that has no `@role`.

```json
{ "method": "_arvel/route/message",
  "params": {
    "task": { "title": "…", "language": "ru" },
    "message": "The signature test still fails, take a look",
    "currentRoleId": "reviewer",
    "candidates": [ { "roleId": "developer", "name": "Developer", "description": "…", "kind": "code" }, { "roleId": "reviewer", "name": "Reviewer", "description": "…", "kind": "check" } ]
} }
```

Result: `{ "roleId": "developer", "confidence": 0.82, "probabilities": { "developer": 0.82, "reviewer": 0.18 } }`. `roleId` MUST be one of `candidates` (the roles of this task plus on-call roles). Below the threshold the host keeps `currentRoleId` and shows the suggestion.

### 14.5 `_arvel/route/effort` (request)

Called before a turn only when the role's effort is **Auto** and the model reports efforts.

```json
{ "method": "_arvel/route/effort",
  "params": { "text": "…", "roleId": "developer", "kind": "code", "levels": ["low", "medium", "high"] } }
```

Result: `{ "effort": "medium", "confidence": 0.7 }`. `effort` MUST be one of `levels`. Below the threshold the host sends no effort (provider default).

### 14.6 The fixed order and the built-in type router (host, informative)

- **Order** (never changes): `lead_intake` → `architect` → `developer` → `reviewer` → `security` → `tester` → `lead_accept`; the `keeper` role runs in the background after a merge. A plugin role is placed right after a chosen slot or is on call only (§16).
- **Plan:** the task type chosen by the user (default: the workspace's last used code type, initially `feature`) ticks its `on` slots; `auto` slots are decided by the host's rules during the task; the user can tick or untick any role for one task.
- **Message:** goes to the role that currently works, or that worked last.
- **Effort:** Auto = no effort sent.

### 14.7 Timeouts and failure

- Each route request has a **3 s** deadline on the host side (configurable down, not up). On timeout, error or invalid result, the host uses the type router for that decision, logs it, and shows a small "router unavailable" chip; it never blocks task creation.
- When its service is unreachable, a router SHOULD fail fast with `provider_error` instead of waiting for the deadline.
- Errors use §9 codes. `rate_limited` and `auth_required` are shown on the router's settings page.

### 14.8 Optional: `_arvel/route/explain` (request)

If `features` includes `route.explain`, the host may call it from the "Try it" panel of the router's settings page with the same params as `route.plan` and shows `{ "lines": [ { "text": "…", "value": 0.82 } ] }` verbatim. Never called during real tasks.

### 14.9 Conformance for routers

`arvel-plugin-check --level release` with a router binary runs the router suite instead of the session suite: manifest `type`, `initialize` < 5 s with empty `agentCapabilities`, `session/new` → `-32601`, every declared `route.*` feature answers the scripted scenarios with ids and slots only from the request and confidences in `[0, 1]`, a canary secret never appears in output, and a request with a 3 s deadline returns or fails in time.

---

## 15. Workspace sandbox (host-enforced)

A workspace is a sandbox. Its purpose is **to protect the computer and the user's data without getting in the way of normal development work**. The host — not the plugin and not the vendor CLI — guarantees it, the same way for every provider. The plugin process, the vendor CLI and every process they start (shell commands, MCP servers over stdio, test runners, dev servers) run inside it.

### 15.0 Levels (per workspace)

| Level | For | Summary |
|---|---|---|
| **Обычная** (default) | everyday work | Files and network as in §15.2–§15.3 with the convenient defaults: package registries, documentation and GitHub read-only are allowed without asking; other hosts → ask. Docker and localhost services are opt-in once per workspace. |
| **Строгая** | unfamiliar code, untrusted repositories | Only the vendor API hosts; every other host is asked; no package caches shared with other workspaces; Docker off. |
| **Доверенная** | the user's own projects when full freedom is needed | `mode: none` — no OS sandbox, only role rules and vendor controls. A permanent warning chip in the UI. |

On every level (including «Доверенная», as far as role rules can enforce it): agents never write to the main checkout or the main branch, never get the user's git/SSH/cloud credentials, and every result stays in the task branch.

### 15.1 Modes

| `mode` | Where | Mechanism (chosen by spike P9 in the app's M0) |
|---|---|---|
| `os` | macOS, Linux (desktop and server); levels «Обычная» and «Строгая» | macOS: Seatbelt profile generated per workspace. Linux: Landlock for files plus a network namespace or seccomp filter that allows only the proxy; `bubblewrap` when available. |
| `container` | any OS with Docker/Podman; the default for Windows when available | The workspace's dev container (or a generic one) with the projects and worktrees mounted; the network only through the proxy. |
| `none` | Windows without a container, or the user turned isolation off | Vendor-native controls only (permission rules, the CLI's own sandbox). The UI shows a red «без изоляции» chip on the workspace and on every task. |

### 15.2 Files

- **Read:** the workspace's projects, its task worktrees, the plugin's install and data dirs, `sandbox.vendorPaths.read`, a per-session temp dir, the system and toolchain locations needed to run programs (the host keeps that list per OS: `/usr`, `/opt/homebrew`, SDKs, `~/go`, `~/.nvm`, `~/.composer`, …), and safe user configs (`~/.gitconfig` without credential helpers, `~/.editorconfig`). `~/.npmrc`, `~/.netrc`, `~/.pypirc` and similar token-bearing files are not readable.
- **Write:** the workspace's worktree root (all tasks of this workspace), the session temp dir, the plugin data dir, `sandbox.vendorPaths.write`, and package caches if the workspace setting «общие кэши пакетов» is on (default on: `~/.npm`, `~/go/pkg/mod`, `~/.cache/go-build`, `~/.composer/cache`, `~/.cache/pip`). The main checkout of a project is read-only for agents.
- **Always denied**, even under an allowed parent: `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.kube`, `~/.config/gcloud`, `~/.docker`, browser profiles, other workspaces' projects and worktrees, Arvel's data dir except the plugin's own, `/var/run/docker.sock` and other container sockets.
- **Secret files of the project** (`.env*` except `.env.example`/`.env.sample`, `*.pem`, `*.key`, `id_*`, `*.p12`) are denied for reading by default; a role can allow them per project.

### 15.3 Network

- Sandboxed processes can connect only to the host's **egress proxy** (`sandbox.proxy`) and to localhost ports of this workspace's dev servers and of the Arvel MCP server. No direct DNS.
- The proxy decides by host name (HTTP CONNECT; no TLS interception):
  1. the plugin's `sandbox.vendorHosts` → always allowed (the CLI must reach its API; the provider's own web search runs there);
  2. the role's network rule: `allow` → the role's and the workspace's allow-lists apply, plus hosts of the role's enabled HTTP MCP servers; `ask` → an approval `network` with the domain («разрешить для задачи / для роли / всегда в пространстве»), the connection waits up to 60 s; `deny` → refused;
  3. anything else → refused and shown in the feed as «заблокировано: соединение с example.com».
- Workspace presets the user can add with one click: package registries (npm, Packagist, Go proxy, PyPI, crates), GitHub read-only (`codeload.github.com`, `raw.githubusercontent.com`), documentation sites.

### 15.3a Local services and Docker

- **Localhost services** (a test database, Redis, a mail catcher): the workspace lists them as `host:port` with a label («тестовая база»). Sandboxed processes may connect to listed ports and to dev servers started inside this workspace. Other local ports are refused.
- **Docker**: the Docker socket gives root-equivalent access to the machine, so it is **off** by default. A workspace can enable it once («Разрешить Docker — агенты смогут запускать контейнеры, это почти полный доступ к компьютеру»). On «Строгая» it cannot be enabled.

### 15.3b Git

- **Local git works fully** inside the sandbox: status, diff, log, blame, branches, commits in the task branch, stash, rebase of the task branch. The worktree's `.git` link and the project's git object store are readable and writable as git needs; refs of `main` and of other tasks' branches are protected by a host-installed `pre-commit`/`reference-transaction` hook in the worktree (writes to them are rejected).
- **Remote operations** need credentials, which never enter the sandbox. They are done **by the host** on the agent's request through tools of the Arvel MCP server:
  - `git_fetch` — fetch from the project's configured remotes (any role; default: allowed);
  - `git_push` — push **only the task branch** to the project's remote. Only the `lead` role at acceptance may call it, under the workspace's task-finishing rule «Отправка ветки»: No / Ask / Yes (default Ask). `main` and protected branches are never pushed by agents;
  - `pr_open` — open a pull request for the task branch via the provider of the remote, if the host has a token for it. Only `lead` at acceptance, under the finishing rule «Pull request»: None / Draft / Ready.
  A plain `git push` inside the sandbox fails with a clear message («используйте инструмент Arvel git_push»), which the role instructions also say.

### 15.3c Processes and resources

- Sandboxed processes cannot signal or inspect processes outside the sandbox, cannot `sudo`/`su`, and cannot write to system locations (global package installs fail; the agent asks the user instead).
- On Linux the host puts each workspace into a cgroup with CPU and memory limits (HostSettings); on macOS limits are advisory (the host watches and warns).
- Disk: the host warns when a workspace's worktrees exceed a configurable size.

### 15.4 Environment and credentials

- The host removes `SSH_AUTH_SOCK`, `GIT_ASKPASS`, `GH_TOKEN`, `GITHUB_TOKEN`, cloud credential variables and git credential helpers from the environment. Remote git goes through the host (§15.3b).
- `HOME` stays the real home (CLIs need it); the deny list above still applies.
- macOS keychain access is allowed only if the manifest sets `macosKeychain: true`, and the install dialog says so.

### 15.5 Nested sandboxes

Some vendor CLIs start their own OS sandbox (Codex on every OS, Claude's Bash sandbox). Whether it runs nested inside the host's sandbox is checked per OS in spike P9 and recorded in each plugin's FINDINGS. If it cannot nest, the plugin turns the vendor sandbox off **only** when `sandbox.mode` is `os` or `container`; with `none` it keeps it on.

### 15.6 Violations

The host records every denied file or network access (sandbox log, proxy log) with the session and shows it in the task feed. Repeated attempts to reach denied paths trigger the loop guard (app M6).

### 15.7 What is not sandboxed

The user's own terminal in the Code view, merges done by the host, and the host itself. The UI labels the user's terminal «ваш терминал — без песочницы».

---

## 16. Declarative plugins: roles, task types, MCP servers, skills

`team` and `integration` plugins have **no process of their own** unless they ship an MCP server. Everything they add is data in the manifest's `contributes` block plus files in the release archive. The host validates it at install time and shows it in the install dialog. The built-in roles, the fixed order and the built-in task types cannot be changed by any plugin.

### 16.1 Manifest

```json
{
  "id": "godot", "name": "Godot", "type": "integration", "version": "0.3.0", "pluginApi": "1",
  "description": "Godot scenes for Arvel agents: an MCP server, skills and the «Game scene» task type.",
  "entry": { "darwin-arm64": { "asset": "arvel-plugin-godot_darwin_arm64.tar.gz", "bin": "godot-mcp" } },
  "checksums": "checksums.txt",
  "permissions": { "spawn": ["godot"], "network": [], "files": "session-cwd" },
  "contributes": {
    "roles": [
      { "id": "scene-designer", "name": { "en": "Scene designer", "ru": "Дизайнер сцен" },
        "description": { "en": "Builds and checks Godot scenes…", "ru": "…" },
        "kind": "code", "placement": { "after": "developer" },
        "instructions": { "en": "roles/scene-designer.en.md", "ru": "roles/scene-designer.ru.md" },
        "permissions": { "read": "allow", "edit": "allow", "commands": { "default": "ask", "allow": ["godot --headless *"] }, "network": "ask" },
        "icon": "palette" }
    ],
    "taskTypes": [
      { "id": "game-scene", "name": { "en": "Game scene", "ru": "Игровая сцена" },
        "description": { "en": "A scene, its scripts and a screenshot check", "ru": "…" },
        "roles": { "lead_intake": "on", "architect": "on", "developer": "on", "security": "auto", "lead_accept": "on", "godot:scene-designer": "on" },
        "branch": "new", "result": "code" }
    ],
    "mcpServers": [
      { "name": "godot", "command": ["{bin}", "--stdio"], "tools": ["scene_tree", "run_scene", "screenshot"] }
    ],
    "skills": [ { "name": "godot-scenes", "path": "skills/godot-scenes" } ]
  }
}
```

### 16.2 Roles

- `id` `^[a-z][a-z0-9-]{1,39}$`; the host stores it as `<pluginId>:<id>`.
- `kind`: `plan`, `code`, `check` or `read` (answers and reports, no edits). `intake`, `accept` and `knowledge` are reserved for the built-in roles.
- `placement`: `{ "after": "<slot>" }` — the role runs right after that slot when a task includes it; several plugin roles after the same slot run in install order. `{ "onCall": true }` — the role is never in the order; it is reached by `@role` in a task and as a subagent target.
- `instructions`: Markdown files in the release archive, at most 16 KiB each, appended to the host's common role preamble. The user's additional instructions are added after them.
- `permissions`: the role's defaults, in the §7.1 shape. The user can change them. A `kind: read` role never gets `edit: allow`.
- A `check` role's findings are handled like the built-in check roles («если есть замечания»).

### 16.3 Task types

- `id` as for roles; stored as `<pluginId>:<id>`.
- `roles`: a map of slots (built-in slots and `<pluginId>:<roleId>` of roles from **this** plugin) to `on` | `off` | `auto` | `optional`. `auto` is valid only for `security`. `lead_accept` MUST be `on` for `result: code`.
- `branch`: the default branch mode — `new` | `continue` | `view` (read-only) | `ask`.
- `result`: `code` (commits in the task branch), `report` (a read-only answer; every role works on a read-only view) or `subtasks` (the type produces a plan of subtasks, like the built-in `spec` type).

### 16.4 MCP servers and skills

- `mcpServers[]`: stdio servers started from the plugin's own release binary (`{bin}` is the entry binary) or an HTTP `url`. The host adds them to its catalogue of agent tools with the scope «из плагина», shows the first-run review like any MCP server, runs them inside the workspace sandbox (§15) and lets roles opt in per tool. A plugin cannot assign its servers to roles itself.
- `skills[]`: Agent Skills folders inside the release archive, added to the catalogue with the scope «из плагина», delivered through §7.1 `skills`.
- `team` plugins MUST NOT have `entry`, `mcpServers` or `skills`; `integration` plugins MUST have at least one MCP server or skill.

### 16.5 Validation and conformance

`arvel-plugin-check --manifest arvel.json` (no binary needed for `team`) validates `contributes`: ids, kinds, placements pointing at existing slots, type maps using only known slots, `auto` only for `security`, instruction files present and within size, localisation keys `en` (required) and `ru` (optional). For `integration` plugins with an MCP server binary it also starts the server and lists its tools over MCP.

