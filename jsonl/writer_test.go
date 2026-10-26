// SPDX-License-Identifier: Apache-2.0

package jsonl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// recordingWriter keeps a copy of every Write call. It has no lock of its own:
// the Writer mutex must protect it, so -race fails if the mutex is missing.
type recordingWriter struct{ writes [][]byte }

func (r *recordingWriter) Write(p []byte) (int, error) {
	r.writes = append(r.writes, append([]byte(nil), p...))
	return len(p), nil
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

type errWriter struct{ err error }

func (e errWriter) Write([]byte) (int, error) { return 0, e.err }

func TestWriter_WritesOneLine(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.WriteJSON(map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "{\"a\":1}\n" {
		t.Fatalf("got %q", got)
	}
	buf.Reset()
	if err := w.WriteJSON(struct{ S string }{"<b>&"}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "{\"S\":\"<b>&\"}\n" {
		t.Fatalf("HTML escaped: %q", got)
	}
}

func TestWriter_RawMessageCompacted(t *testing.T) {
	var buf bytes.Buffer
	if err := NewWriter(&buf).WriteJSON(json.RawMessage("{\n \"a\": 1\n}")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "{\"a\":1}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestWriter_ConcurrentLinesStayWhole(t *testing.T) {
	rec := &recordingWriter{}
	w := NewWriter(rec)
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if err := w.WriteJSON(map[string]any{"g": g, "i": i, "pad": "x"}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if len(rec.writes) != 5000 {
		t.Fatalf("recorded %d writes, want 5000", len(rec.writes))
	}
	for _, line := range rec.writes {
		if !bytes.HasSuffix(line, []byte("\n")) || bytes.Count(line, []byte("\n")) != 1 {
			t.Fatalf("not exactly one line: %q", line)
		}
		var v map[string]any
		if err := json.Unmarshal(line, &v); err != nil {
			t.Fatalf("invalid JSON %q: %v", line, err)
		}
	}
}

func TestWriter_MarshalErrorWritesNothing(t *testing.T) {
	rec := &recordingWriter{}
	if err := NewWriter(rec).WriteJSON(make(chan int)); err == nil {
		t.Fatal("expected a marshal error")
	}
	if len(rec.writes) != 0 {
		t.Fatalf("wrote %d times", len(rec.writes))
	}
}

func TestWriter_ShortWrite(t *testing.T) {
	err := NewWriter(shortWriter{}).WriteJSON(1)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("err = %v", err)
	}
}

func TestWriter_PropagatesWriteError(t *testing.T) {
	err := NewWriter(errWriter{io.ErrClosedPipe}).WriteJSON(1)
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("err = %v", err)
	}
}

func TestWriter_FlushesBufferedWriter(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(bufio.NewWriter(&buf))
	if err := w.WriteJSON("x"); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "\"x\"\n" {
		t.Fatalf("not flushed: %q", buf.String())
	}
}

func TestWriter_OversizeLine(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates 64 MiB")
	}
	rec := &recordingWriter{}
	err := NewWriter(rec).WriteJSON(strings.Repeat("x", DefaultMaxLine))
	if !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("err = %v", err)
	}
	if len(rec.writes) != 0 {
		t.Fatal("oversize message was written")
	}
}

func TestWriterScanner_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	for i := 0; i < 100; i++ {
		if err := w.WriteJSON(map[string]any{"n": i, "s": "line <" + strings.Repeat("é", i) + ">"}); err != nil {
			t.Fatal(err)
		}
	}
	sc := NewScanner(&buf, 0)
	n := 0
	for sc.Scan() {
		var v struct {
			N int    `json:"n"`
			S string `json:"s"`
		}
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if v.N != n || v.S != "line <"+strings.Repeat("é", n)+">" {
			t.Fatalf("message %d corrupted: %+v", n, v)
		}
		n++
	}
	if n != 100 || sc.Err() != nil {
		t.Fatalf("read %d messages, err=%v", n, sc.Err())
	}
}
