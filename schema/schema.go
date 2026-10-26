// SPDX-License-Identifier: Apache-2.0

// Package schema embeds the JSON Schema of arvel.json. The file lives here
// because go:embed cannot reach a parent directory.
package schema

import (
	_ "embed"
)

// ID is the $id of the schema, the URL that manifests name in "$schema".
const ID = "https://raw.githubusercontent.com/arvel-dev/arvel-sdk/main/schema/arvel.schema.json"

//go:embed arvel.schema.json
var raw []byte

// JSON returns a copy of the embedded schema document.
func JSON() []byte {
	return append([]byte(nil), raw...)
}
