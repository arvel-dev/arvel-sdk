// SPDX-License-Identifier: Apache-2.0

// Package skills delivers the Agent Skills folders the Arvel host prepares for a
// session (PROTOCOL §7.1 item 5a) to a vendor CLI that reads project skills
// from a directory inside the session's working directory.
//
// Materialize copies each folder into the first project skills directory, hides
// the copies from git through the worktree's info/exclude, prefixes a folder
// with "arvel-" when the repository already has a skill of that name, turns
// invocation "manual" into disable-model-invocation: true, and returns a
// cleanup function that removes only that call's uniquely marked copies. Source
// folders are only read. Existing marked copies are never reclaimed; if both
// names are occupied Materialize returns ErrNameTaken. Abandoned copies need
// separate recovery after proving that no active task uses them. Use isolated
// session working directories when skill selections differ. Materialize does
// not disable external/global vendor skills or manage host catalogue selections.
//
// Materialize starts the git binary (through procx) to find info/exclude, so a
// plugin that calls it must list "git" in permissions.spawn of its manifest.
// Without git, or outside a git work tree, the copies are still made but
// nothing is excluded.
package skills
