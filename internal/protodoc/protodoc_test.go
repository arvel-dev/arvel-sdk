// SPDX-License-Identifier: Apache-2.0

package protodoc

import (
	"strings"
	"testing"
)

const sample = "text\n\n```json\n{ \"a\": \"https://x//y\" } // trailing\n```\n\n- item\n   ```json\n   {\"b\": 1}\n   ```\n\n```json\n\"entry\": { \"c\": [1] }\n```\n\n```json\n// params\n{ \"id\": 1 }\n/* two */\n{ \"id\": 2 }\n```\n\n```go\nnot json\n```\n"

func TestJSONBlocks(t *testing.T) {
	blocks, err := JSONBlocks(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 4 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	if blocks[1].Index != 2 || blocks[1].Line != 8 || strings.TrimSpace(blocks[1].Text) != `{"b": 1}` {
		t.Errorf("indented block: %+v", blocks[1])
	}
}

func TestValues(t *testing.T) {
	blocks, _ := JSONBlocks(strings.NewReader(sample))
	want := []int{1, 1, 1, 2}
	for i, b := range blocks {
		vs, err := b.Values()
		if err != nil {
			t.Fatalf("block %d: %v", i+1, err)
		}
		if len(vs) != want[i] {
			t.Errorf("block %d: %d values, want %d", i+1, len(vs), want[i])
		}
	}
	vs, _ := blocks[0].Values()
	if !strings.Contains(string(vs[0]), "https://x//y") {
		t.Errorf("URL damaged: %s", vs[0])
	}
	vs, _ = blocks[2].Values()
	if string(vs[0]) != `{"entry": { "c": [1] }}` {
		t.Errorf("fragment: %s", vs[0])
	}
}

func TestStripComments(t *testing.T) {
	got := StripComments("a // x\nb /* y */ c \"//\" \"\\\"//\"")
	if got != "a \nb   c \"//\" \"\\\"//\"" {
		t.Errorf("got %q", got)
	}
	if StripComments("x /* open") != "x " {
		t.Error("unterminated block comment")
	}
}

func TestErrors(t *testing.T) {
	if _, err := JSONBlocks(strings.NewReader("```json\n{")); err == nil {
		t.Error("unterminated block accepted")
	}
	b := Block{Index: 1, Line: 1, Text: "// only a comment"}
	if _, err := b.Values(); err == nil {
		t.Error("empty block accepted")
	}
	b = Block{Index: 1, Line: 1, Text: "{bad"}
	if _, err := b.Values(); err == nil {
		t.Error("bad JSON accepted")
	}
}
