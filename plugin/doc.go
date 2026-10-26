// SPDX-License-Identifier: Apache-2.0

// Package plugin is the framework for Arvel plugins. A vendor plugin
// implements Provider and Session and calls Run; a router plugin implements
// Router (plus PlanRouter, MessageRouter, EffortRouter, ExplainRouter as it
// needs) and calls RunRouter. The framework does everything the protocol asks
// of every plugin: the stdio transport, initialize, the _arvel/* methods, the
// ACP session methods, concurrency rules, permission requests, throttling,
// shutdown, panic recovery, error conversion and secret redaction.
//
// A provider's main function:
//
//	func main() {
//		p := &myProvider{}
//		if err := plugin.Run(context.Background(), p, plugin.WithVersion("arvel-plugin-x", version)); err != nil {
//			os.Exit(1)
//		}
//	}
//
// Run returns nil after a graceful end, so the process exits 0.
//
// Errors a plugin returns reach the host as follows: a *protocol.RPCError built
// with protocol.NewError keeps its code, anything else becomes the Arvel error
// "provider_error", and the text of every error is scrubbed of the secrets the
// host sent in initialize.
package plugin
