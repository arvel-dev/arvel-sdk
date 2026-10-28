# arvel-plugin-team-example

[![Release](https://img.shields.io/github/v/release/YOUR_ORG/arvel-plugin-team-example)](https://github.com/YOUR_ORG/arvel-plugin-team-example/releases)
[![License](https://img.shields.io/badge/license-MIT--0-blue.svg)](LICENSE)

Adds YOUR_ROLES and YOUR_TASK_TYPES to Arvel. Choose `team` for roles/types only,
or `integration` when supplying MCP servers or skills. Replace all example
identifiers before publishing.

## What it adds

| Role ID | Kind | Placement | When needed |
|---|---|---|---|
| `docs-writer` | `code` | After `tester` | Documentation changes |

| Task type | Participating slots | Branch | Result |
|---|---|---|---|
| `docs` | Intake, plugin docs writer, reviewer, acceptance | `new` | `code` |

Runtime plugin slots are namespaced as `PLUGIN_ID:ROLE_ID`. Explain defaults and
the order placement, without renaming or reordering built-in roles. Every
code-result type must keep `lead_accept: on`; `auto` is for `security` only.

## Permissions

Describe each role's read, edit, commands and network defaults. The sample asks
before edits and denies network; a sentence telling the agent to edit only
`docs/` is an instruction, not a filesystem permission rule. Explain any MCP
server's spawn/network access and which operations can change user data.

## MCP servers and skills

For an integration, list server names, transports, commands or remote URLs and
the tools they expose. Explain where a stdio executable comes from. Document
authentication setup separately: MCP contributions expose no env/header or
credential-reference fields. Never put keys in manifest commands or URLs.
List skill directories, invocation modes and permissions. Remove this section
only for a team with no MCP servers or skills.
Identify the integration plugin as source/owner of its skill originals. Explain
how spaces and roles opt into the relevant provider's separate catalogue, without
automatic merging across providers. Disabling/removing a space selection must
preserve packaged/external originals and copies used by other active tasks.
External/global vendor CLI skills may remain available; permissions and MCP
policy are unchanged.

## Compatibility

State `pluginApi: "1"`, tested Arvel versions and supported server platforms.
English names, descriptions and role instructions are required; list optional
translations. Each role instruction file is at most 16 KiB.

## Development and releases

Start from `examples/team-sample/arvel.json` and `roles/`. Copy
`templates/scripts/release-notes.sh` into `scripts/release-notes.sh`, and copy
`templates/CHANGELOG.md` to the repository root. Workflows run from that root.

```sh
arvel-plugin-check --manifest arvel.json --package .
```

For a **team**, copy `templates/.github/workflows/release.team.yml` to
`.github/workflows/release.yml`. Rename `PLUGIN_NAME` there. It validates the
tag-versioned package, packs data only and creates a draft with `arvel.json`, a
data archive and `checksums.txt`. It compiles no plugin. `examples/team-sample/release.yml`
is the same workflow with the sample package name.

For an **integration** with stdio MCP binaries, use six-platform packaging and
add the referenced skill/instruction files to every archive. Adapt **both** the
copied CI and release conformance commands to include `--package .`. CI uses
`--level basic`; the release check starts the server and lists tools:

```sh
arvel-plugin-check --level release --manifest arvel.json --package . -- ./bin/my-mcp-server
```

For remote MCP/skills-only integrations, the manifest/package command is enough
to validate files and declarations; remote tool listing is reported as skipped.
Adapt the data archive file list to include skills. A team workflow cannot be
used unchanged for an integration that needs executables.

Move Unreleased notes into a dated `X.Y.Z` section before tagging `vX.Y.Z`.
Review the draft, notes, files and checksums, then publish. `-beta.N` tags are
prereleases. Add topics `arvel-plugin` and `arvel-team` or `arvel-integration`.

## License

Name the license for instructions, skills and server binaries. SDK templates
and examples are MIT-0.
