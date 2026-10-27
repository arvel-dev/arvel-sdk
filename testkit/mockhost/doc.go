// SPDX-License-Identifier: Apache-2.0

// Package mockhost is the scripted ACP client of the testkit (SPEC §5.9). It
// drives a provider or a router plugin either in-process (plugin.Run /
// plugin.RunRouter over pipes) or as a started binary, mirrors the protocol
// methods, collects session/update notifications, auto-answers permission
// requests, and runs scenario files.
package mockhost
