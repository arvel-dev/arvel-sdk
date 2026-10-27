// SPDX-License-Identifier: Apache-2.0

// Package scenario defines the YAML scenario format shared by the SDK tests and
// arvel-plugin-check (SPEC §5.9). A scenario describes an exchange with a
// plugin: an optional environment, the initialize connection, and an ordered
// list of steps, each a call whose result is asserted or partly saved into
// variables for later steps.
//
// Placeholders {{tmpdir}}, {{name}} and {{fakevendor:name}} are resolved by a
// Resolver before a step runs. Saves use a dotted JSONPath; expects are
// recursive JSON-subset matches; expectUpdates matches collected session/update
// notifications; answerPermissions arms the mock host to auto-answer a
// session/request_permission during the step.
package scenario
