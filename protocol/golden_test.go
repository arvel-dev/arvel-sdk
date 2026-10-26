// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arvel-dev/arvel-sdk/internal/jsoneq"
	"github.com/arvel-dev/arvel-sdk/internal/protodoc"
)

const protocolDoc = "../docs/PROTOCOL.md"

// example binds one golden file to a typed value.
type example struct {
	file     string // under protocol/testdata
	block    int    // 1-based fenced json block of PROTOCOL.md, 0 for inline examples
	value    int    // index of the JSON value inside the block
	inline   string // exact text of an inline example in PROTOCOL.md
	ptr      string // JSON pointer to the part decoded into the typed value
	decode   func([]byte) (any, error)
	validate func(any) error
}

func dec[T any]() func([]byte) (any, error) {
	return func(raw []byte) (any, error) {
		var v T
		err := json.Unmarshal(raw, &v)
		return &v, err
	}
}

func val[T interface{ Validate() error }](v any) error { return (*(v.(*T))).Validate() }

var examples = []example{
	{file: "5.1-initialize-request.json", block: 4, ptr: "/params/_meta/arvel", decode: dec[InitMeta]()},
	{file: "5.1-initialize-response.json", block: 5, ptr: "/result/_meta/arvel", decode: dec[InitResultMeta]()},
	{file: "6.1-status-result.json", block: 6, decode: dec[Status](), validate: val[Status]},
	{file: "6.2-models-params.json", block: 7, value: 0, decode: dec[ModelsParams]()},
	{file: "6.2-models-result.json", block: 7, value: 1, decode: dec[ModelsResult](), validate: val[ModelsResult]},
	{file: "6.3-usage.json", block: 8, decode: dec[Usage](), validate: val[Usage]},
	{file: "6.4-action-params.json", block: 9, value: 0, decode: dec[ActionParams]()},
	{file: "6.4-action-result-terminal.json", block: 9, value: 1, decode: dec[ActionResult](), validate: val[ActionResult]},
	{file: "6.4-action-result-url.json", block: 9, value: 2, decode: dec[ActionResult](), validate: val[ActionResult]},
	{file: "6.4-action-result-done.json", block: 9, value: 3, decode: dec[ActionResult](), validate: val[ActionResult]},
	{file: "7.1-session-new.json", block: 10, ptr: "/params/_meta/arvel", decode: dec[SessionMeta](), validate: val[SessionMeta]},
	{file: "7.2-turn-meta.json", block: 11, ptr: "/_meta/arvel", decode: dec[TurnMeta]()},
	{file: "7.3-request-permission.json", block: 12, ptr: "/params/_meta/arvel", decode: dec[PermissionMeta](), validate: val[PermissionMeta]},
	{file: "7.9-discover-request.json", block: 13, ptr: "/params", decode: dec[DiscoverParams]()},
	{file: "7.9-discover-result.json", block: 14, ptr: "/result", decode: dec[DiscoverResult](), validate: val[DiscoverResult]},
	{file: "9-error-data.json", block: 15, decode: dec[ErrorData](), validate: func(v any) error { return v.(*ErrorData).Validate() }},
	{file: "14.3-route-plan-request.json", block: 16, ptr: "/params", decode: dec[RoutePlanParams](), validate: val[RoutePlanParams]},
	{file: "14.3-route-plan-result.json", block: 17, ptr: "/result", decode: dec[RoutePlanResult]()},
	{file: "14.4-route-message-request.json", block: 18, ptr: "/params", decode: dec[RouteMessageParams]()},
	{file: "14.5-route-effort-request.json", block: 19, ptr: "/params", decode: dec[RouteEffortParams]()},
	{file: "14.4-route-message-result.json", inline: `{ "roleId": "developer", "confidence": 0.82, "probabilities": { "developer": 0.82, "reviewer": 0.18 } }`, decode: dec[RouteMessageResult]()},
	{file: "14.5-route-effort-result.json", inline: `{ "effort": "medium", "confidence": 0.7 }`, decode: dec[RouteEffortResult]()},
	{file: "14.8-route-explain-result.json", inline: `{ "lines": [ { "text": "…", "value": 0.82 } ] }`, decode: dec[RouteExplainResult]()},
}

// manifestBlocks are the examples of PROTOCOL.md that the manifest package
// binds to its types (manifest/testdata).
var manifestBlocks = map[int]string{
	1:  "../manifest/testdata/section-3.2.json",
	2:  "../manifest/testdata/section-3.2-command-entry.json",
	3:  "../manifest/testdata/section-4-connections.json",
	20: "../manifest/testdata/section-16.1.json",
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func docBlocks(t *testing.T) []protodoc.Block {
	t.Helper()
	f, err := os.Open(protocolDoc)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	blocks, err := protodoc.JSONBlocks(f)
	if err != nil {
		t.Fatal(err)
	}
	return blocks
}

// pick returns the part of raw addressed by a JSON pointer made of object keys.
func pick(t *testing.T, raw []byte, ptr string) []byte {
	t.Helper()
	if ptr == "" {
		return raw
	}
	var cur any
	if err := json.Unmarshal(raw, &cur); err != nil {
		t.Fatal(err)
	}
	for _, key := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		m, ok := cur.(map[string]any)
		if !ok || m[key] == nil {
			t.Fatalf("pointer %s: no member %q", ptr, key)
		}
		cur = m[key]
	}
	b, err := json.Marshal(cur)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGoldenRoundTrip(t *testing.T) {
	for _, ex := range examples {
		t.Run(ex.file, func(t *testing.T) {
			part := pick(t, readFile(t, filepath.Join("testdata", ex.file)), ex.ptr)
			v, err := ex.decode(part)
			if err != nil {
				t.Fatal(err)
			}
			back, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if ok, diff := jsoneq.Equal(part, back); !ok {
				t.Fatalf("round trip differs: %s\nwant %s\ngot  %s", diff, part, back)
			}
			if ex.validate != nil {
				if err := ex.validate(v); err != nil {
					t.Fatalf("validate: %v", err)
				}
			}
		})
	}
}

func TestGoldenMatchesDocument(t *testing.T) {
	blocks := docBlocks(t)
	docText, err := os.ReadFile(protocolDoc)
	if err != nil {
		t.Fatal(err)
	}
	for _, ex := range examples {
		golden := readFile(t, filepath.Join("testdata", ex.file))
		if ex.inline != "" {
			if !strings.Contains(string(docText), ex.inline) {
				t.Errorf("%s: inline example not found in PROTOCOL.md", ex.file)
			}
			if ok, diff := jsoneq.Equal(golden, []byte(ex.inline)); !ok {
				t.Errorf("%s: differs from inline example: %s", ex.file, diff)
			}
			continue
		}
		values, err := blocks[ex.block-1].Values()
		if err != nil {
			t.Fatal(err)
		}
		if ok, diff := jsoneq.Equal(golden, values[ex.value]); !ok {
			t.Errorf("%s: differs from PROTOCOL.md block %d: %s", ex.file, ex.block, diff)
		}
	}
	for block, file := range manifestBlocks {
		values, err := blocks[block-1].Values()
		if err != nil {
			t.Fatal(err)
		}
		if ok, diff := jsoneq.Equal(readFile(t, file), values[0]); !ok {
			t.Errorf("%s: differs from PROTOCOL.md block %d: %s", file, block, diff)
		}
	}
}

func TestEveryDocumentExampleHasGolden(t *testing.T) {
	blocks := docBlocks(t)
	covered := map[int]bool{}
	for _, ex := range examples {
		if ex.block > 0 {
			covered[ex.block] = true
		}
	}
	for b := range manifestBlocks {
		covered[b] = true
	}
	for _, b := range blocks {
		if !covered[b.Index] {
			t.Errorf("PROTOCOL.md json block %d (line %d) has no golden file; add it to the examples table", b.Index, b.Line)
		}
	}
	if len(covered) != len(blocks) {
		t.Errorf("table covers %d blocks, document has %d", len(covered), len(blocks))
	}

	referenced := map[string]bool{}
	for _, ex := range examples {
		referenced[ex.file] = true
	}
	files, err := filepath.Glob("testdata/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if !referenced[filepath.Base(f)] {
			t.Errorf("%s is not referenced by the examples table", f)
		}
	}
}

func TestGoldenPlanResultValidatesAgainstRequest(t *testing.T) {
	var req RoutePlanParams
	if err := json.Unmarshal(pick(t, readFile(t, "testdata/14.3-route-plan-request.json"), "/params"), &req); err != nil {
		t.Fatal(err)
	}
	var res RoutePlanResult
	if err := json.Unmarshal(pick(t, readFile(t, "testdata/14.3-route-plan-result.json"), "/result"), &res); err != nil {
		t.Fatal(err)
	}
	if err := res.Validate(req); err != nil {
		t.Fatal(err)
	}
	clean, ignored := res.Sanitize(req)
	if len(ignored) != 0 || len(clean.Roles) != len(res.Roles) {
		t.Fatalf("sanitize changed the documented result: %+v %v", clean, ignored)
	}

	var msgReq RouteMessageParams
	if err := json.Unmarshal(pick(t, readFile(t, "testdata/14.4-route-message-request.json"), "/params"), &msgReq); err != nil {
		t.Fatal(err)
	}
	var msgRes RouteMessageResult
	if err := json.Unmarshal(readFile(t, "testdata/14.4-route-message-result.json"), &msgRes); err != nil {
		t.Fatal(err)
	}
	if err := msgRes.Validate(msgReq); err != nil {
		t.Fatal(err)
	}

	var effReq RouteEffortParams
	if err := json.Unmarshal(pick(t, readFile(t, "testdata/14.5-route-effort-request.json"), "/params"), &effReq); err != nil {
		t.Fatal(err)
	}
	var effRes RouteEffortResult
	if err := json.Unmarshal(readFile(t, "testdata/14.5-route-effort-result.json"), &effRes); err != nil {
		t.Fatal(err)
	}
	if err := effRes.Validate(effReq); err != nil {
		t.Fatal(err)
	}

	var exp RouteExplainResult
	if err := json.Unmarshal(readFile(t, "testdata/14.8-route-explain-result.json"), &exp); err != nil {
		t.Fatal(err)
	}
	if err := exp.Validate(req); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownFieldsIgnored(t *testing.T) {
	raw := readFile(t, "testdata/6.1-status-result.json")
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["futureField"] = 1
	m["binary"].(map[string]any)["futureNested"] = true
	b, _ := json.Marshal(m)
	var s Status
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("unknown fields must be ignored: %v", err)
	}
	if s.State != StateReady {
		t.Fatalf("state = %q", s.State)
	}
}

func TestGoldenSessionNewTask(t *testing.T) {
	var m SessionMeta
	if err := json.Unmarshal(pick(t, readFile(t, "testdata/7.1-session-new.json"), "/params/_meta/arvel"), &m); err != nil {
		t.Fatal(err)
	}
	want := TaskMeta{Number: 42, Code: "SHOP-1287", Type: "feature", ReadOnly: false}
	if m.Task == nil || *m.Task != want {
		t.Fatalf("task = %+v, want %+v", m.Task, want)
	}
	if got := m.Normalize().Permissions.Edit; got != PermissionLevelAllow {
		t.Fatalf("edit = %q, want allow", got)
	}
}
