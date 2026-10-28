# Writing a team or integration plugin

Declarative plugins contribute data, with no provider/router process. A `team`
adds roles and task types. An `integration` adds at least one MCP server or skill,
and may also add roles/types. Only a stdio MCP server needs an executable.
See [PROTOCOL §16](PROTOCOL.md#16-declarative-plugins-roles-task-types-mcp-servers-skills)
and the complete [`examples/team-sample`](../examples/team-sample).

## Start with the team sample

From a directory containing an SDK clone:

```sh
mkdir arvel-plugin-myteam
cp arvel-sdk/examples/team-sample/arvel.json arvel-plugin-myteam/
cp -R arvel-sdk/examples/team-sample/roles arvel-plugin-myteam/
cp arvel-sdk/templates/LICENSE arvel-plugin-myteam/
cp arvel-sdk/templates/README.team.md arvel-plugin-myteam/README.md
cp arvel-sdk/templates/CHANGELOG.md arvel-plugin-myteam/
mkdir -p arvel-plugin-myteam/scripts arvel-plugin-myteam/.github/workflows
cp arvel-sdk/templates/scripts/release-notes.sh arvel-plugin-myteam/scripts/
cp arvel-sdk/templates/.github/workflows/release.team.yml arvel-plugin-myteam/.github/workflows/release.yml
cd arvel-plugin-myteam
go install github.com/arvel-dev/arvel-sdk/cmd/arvel-plugin-check@v0.1.2
arvel-plugin-check --manifest arvel.json --package .
```

No `go.mod` or plugin binary is needed. Windows authors can use their shell's
copy commands. The checker itself requires the SDK's current Go minimum, or can
be downloaded as a release binary. Rename the manifest ID/name and workflow
`PLUGIN_NAME`; change references such as `team-sample:docs-writer` together.
The example's `release.yml` is a workflow copy source for a standalone plugin
root, not an active SDK-root workflow. Both workflows expect the notes script at
`scripts/release-notes.sh` in the new repository.

## One role and one task type

The sample role `docs-writer` is `kind: code`, placed after `tester`.
Its English and Russian names/descriptions/instructions appear in `arvel.json`
and [`roles/`](../examples/team-sample/roles). English is required; Russian and
other translations are optional. Instruction files are relative package paths,
each at most 16 KiB. They are appended to the host's common role preamble;
the user's additional instructions follow them.

Allowed plugin role kinds are `plan`, `code`, `check` and `read`. Built-in
`intake`, `accept` and `knowledge` remain reserved. The host stores the role as
`PLUGIN_ID:ROLE_ID`; another plugin's slot cannot be used in your task-type map.
Use `placement.after` for an existing built-in/own slot, with no cycles, or
`placement.onCall: true` for an on-demand role. Plugins do not rename, reorder or
replace built-in roles or task types.

The sample `docs` task type enables `lead_intake`, `team-sample:docs-writer`,
`reviewer` and `lead_accept`, creates a new branch and returns code. Slot states
are `on`, `off`, `optional`, and `auto` only for `security`. A `result: code`
type must keep `lead_accept: on`. Other branch modes are `continue`, `view` and
`ask`; other results are `report` (read-only) and `subtasks`.

## Permissions are separate from instructions

The sample permits reads, asks before edits/commands and denies network. Its
instructions ask the agent to work under `docs/`; that text is not a path-based
access-control rule. Describe the defaults honestly in your README and let the
host's resolved permissions control operations. Users may change role defaults;
read-only tasks and `kind: read` roles cannot gain edit permission from a plugin.

## Integration variants

| Integration content | Manifest declaration | Release check |
|---|---|---|
| Skills | `contributes.skills` with package-relative folders | Validate manifest and package files |
| Remote MCP | `contributes.mcpServers` with an HTTP `url` | Validate declarations; remote tool listing is skipped |
| Stdio MCP | MCP `command` containing `{bin}` and a platform entry binary | At release level, start server and list its tools |

The protocol's integration example shows roles, types, an MCP server and skills
together. A `team` must have none of `entry`, `connections`, MCP servers or skills;
an integration must have at least one server or skill. Package skill folders
inside the archive; reference them by relative path. The host adds them to its
catalogues separately for each provider, recording the integration plugin as
source/owner, and delivers the enabled space/role selections through session
metadata. Catalogues are not merged automatically. Removing a skill from a space
does not delete its packaged original or copies used by other active tasks.
External/global CLI skills may still be available; this does not change MCP or
permission policy. See [PROTOCOL §7.7](PROTOCOL.md#77-provider-skill-catalogues-and-the-hosts-mcp-catalogue).

`{bin}` resolves to the entry executable. The checker accepts an explicit local
build after `--`, which is useful before making platform archives:

```sh
arvel-plugin-check --level release --manifest arvel.json --package . -- ./bin/my-mcp-server
```

Without that binary argument it looks for a binary named after the manifest ID
inside `--package` (`.exe` on Windows). Supply an explicit path when the entry
binary has another name. The checker lists declared/discovered tools; remote
servers are not contacted by this check. MCP contributions expose only name,
command, URL and tool hints; there are no env/header or credential-reference
fields. Document any out-of-band server authentication setup and never embed
keys in manifest commands, URLs or archives. Discovery's `secret`/`envRef`
references belong to a separate protocol surface. The host runs MCP servers in
the workspace sandbox and reviews tools;
a plugin cannot assign its tools to roles itself.

## Validate and release

Manifest validation covers IDs, kinds, slot maps, placement cycles, reserved
roles, localization and instruction sizes/files. `--package` points at the actual
unpacked files, so keep it explicit. Run the command again after substituting
the tag version to validate what users will receive.

For a team, the [data-only workflow](../templates/.github/workflows/release.team.yml)
stages `arvel.json` and `roles/`, validates them, creates a tar archive and hashes
both it and the separate manifest. It installs a checker but builds no plugin.
The draft contains only those two assets and `checksums.txt`. Put nonempty notes
in `## [X.Y.Z] - YYYY-MM-DD` before tagging `vX.Y.Z`; missing/empty notes stop
the workflow. Beta tags create prerelease drafts.

Skills-only/remote-MCP integrations can adapt data packaging by adding skill
folders. Stdio integrations use the binary [GoReleaser template](../templates/.goreleaser.yaml),
including referenced roles/skills in every archive. Change **both** the copied
CI and release workflow conformance commands to include `--package .` with an
explicit server binary. CI uses `--level basic`; release uses the manifest/package
release command above.
Provider/router session tests do not apply to MCP-only binaries. Review every
draft's exact files, version, notes and SHA-256 values before publication; add
catalog topics `arvel-plugin` plus `arvel-team` or `arvel-integration`.
