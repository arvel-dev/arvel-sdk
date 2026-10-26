// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestEmbeddedSchemaIsValidJSON(t *testing.T) {
	var v map[string]any
	if err := json.Unmarshal(JSON(), &v); err != nil {
		t.Fatal(err)
	}
	if v["$id"] != ID {
		t.Fatalf("$id = %v, want %s", v["$id"], ID)
	}
}

func TestJSONReturnsCopy(t *testing.T) {
	a := JSON()
	a[0] = 'X'
	if JSON()[0] == 'X' {
		t.Fatal("JSON must return a defensive copy")
	}
}

func TestSchemaCompiles(t *testing.T) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(JSON()))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(ID, doc); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Compile(ID); err != nil {
		t.Fatal(err)
	}
}
