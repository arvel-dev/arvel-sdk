# Contributing

## Requirements
- Go 1.26 or newer (CI uses the version in `go.mod`).
- Builds are made without CGO.

## Build and test
- `go vet ./...`
- `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...`
- `go run ./internal/tools/spdxcheck`
- `go test -race ./...` (the race detector needs `CGO_ENABLED=1`; release builds use `CGO_ENABLED=0`)

## Licence headers
- The first line of every `.go` file is `// SPDX-License-Identifier: Apache-2.0`.
- Files under `examples/` and `templates/` use `// SPDX-License-Identifier: MIT-0`.
- Leave a blank line between the header and the package comment.

## Protocol changes
- Edit `docs/PROTOCOL.md` in the same commit as the code.
- Add a golden test under `protocol/testdata/`.
- Breaking changes to `pluginApi: "1"` are not accepted.

## API changes
- List every exported API change in `CHANGELOG.md`.
- Breaking changes need a migration note.
- A new dependency needs a written justification in the pull request.

## Layering
- Packages import only the layers below them (see the layout in `README.md`).
- Vendor-specific code does not belong in this module.

## Commits
- Use Conventional Commits: `feat:`, `fix:`, `docs:`, `chore:`, `test:`, `ci:`.

## Local tooling files
- Files of local AI tooling listed in `.gitignore` are never committed.
