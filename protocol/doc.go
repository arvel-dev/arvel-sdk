// SPDX-License-Identifier: Apache-2.0

// Package protocol defines the wire types of the Arvel Provider Protocol v1
// (docs/PROTOCOL.md): the "_meta.arvel" structures, the "_arvel/*" request and
// result bodies, the error model and the router types.
//
// JSON names are copied from the protocol. Decoding ignores unknown fields
// (PROTOCOL §12). The package imports only the standard library.
package protocol
