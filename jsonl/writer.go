// SPDX-License-Identifier: Apache-2.0

package jsonl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Writer writes one JSON value per line. It is safe for concurrent use: every
// call performs a single Write on the underlying writer, so lines never
// interleave.
type Writer struct {
	mu    sync.Mutex
	w     io.Writer
	flush func() error
}

// NewWriter returns a Writer on w. If w has a Flush() error method (for
// example *bufio.Writer) it is called after every line.
func NewWriter(w io.Writer) *Writer {
	wr := &Writer{w: w}
	if f, ok := w.(interface{ Flush() error }); ok {
		wr.flush = f.Flush
	}
	return wr
}

// WriteJSON marshals v and writes it followed by '\n'. HTML characters are not
// escaped. A json.RawMessage is compacted, so the output never contains an
// embedded newline. A message longer than DefaultMaxLine is refused with
// ErrLineTooLong because the peer would drop it (PROTOCOL §2).
func (w *Writer) WriteJSON(v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("jsonl: marshal: %w", err)
	}
	line := buf.Bytes()
	if len(line)-1 > DefaultMaxLine {
		return fmt.Errorf("jsonl: outgoing message of %d bytes: %w", len(line)-1, ErrLineTooLong)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.w.Write(line)
	if err == nil && n < len(line) {
		err = io.ErrShortWrite
	}
	if err == nil && w.flush != nil {
		err = w.flush()
	}
	if err != nil {
		return fmt.Errorf("jsonl: write: %w", err)
	}
	return nil
}
