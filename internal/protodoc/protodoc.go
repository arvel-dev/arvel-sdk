// SPDX-License-Identifier: Apache-2.0

// Package protodoc extracts the JSON examples from docs/PROTOCOL.md so tests
// can bind every example to a typed value.
package protodoc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Block is one fenced json code block of the document.
type Block struct {
	Index int    // 1-based, in document order
	Line  int    // line of the opening fence
	Text  string // content, dedented by the fence indent
}

var (
	openFence  = regexp.MustCompile("^(\\s*)```json\\s*$")
	closeFence = regexp.MustCompile("^\\s*```\\s*$")
)

// JSONBlocks returns every fenced json block, including indented ones.
func JSONBlocks(r io.Reader) ([]Block, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	var (
		blocks []Block
		cur    *Block
		indent string
		body   []string
		line   int
	)
	for sc.Scan() {
		line++
		text := sc.Text()
		if cur == nil {
			if m := openFence.FindStringSubmatch(text); m != nil {
				indent = m[1]
				cur = &Block{Index: len(blocks) + 1, Line: line}
				body = body[:0]
			}
			continue
		}
		if closeFence.MatchString(text) {
			cur.Text = strings.Join(body, "\n")
			blocks = append(blocks, *cur)
			cur = nil
			continue
		}
		body = append(body, strings.TrimPrefix(text, indent))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if cur != nil {
		return nil, fmt.Errorf("protodoc: unterminated json block opened at line %d", cur.Line)
	}
	return blocks, nil
}

// StripComments removes // and /* */ comments outside JSON strings.
func StripComments(s string) string {
	var b strings.Builder
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			if i < len(s) {
				b.WriteByte('\n')
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += end + 3
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Values returns the JSON values of the block after comment removal. A block
// that starts with a string key (a fragment such as `"entry": {...}`) is
// wrapped in braces first.
func (b Block) Values() ([]json.RawMessage, error) {
	text := strings.TrimSpace(StripComments(b.Text))
	if strings.HasPrefix(text, `"`) {
		text = "{" + text + "}"
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	var out []json.RawMessage
	for {
		var v json.RawMessage
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("protodoc: block %d (line %d): %w", b.Index, b.Line, err)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("protodoc: block %d (line %d) holds no JSON value", b.Index, b.Line)
	}
	return out, nil
}
