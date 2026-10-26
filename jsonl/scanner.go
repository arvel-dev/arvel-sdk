// SPDX-License-Identifier: Apache-2.0

// Package jsonl frames newline-delimited JSON, the transport of the Arvel
// Provider Protocol (PROTOCOL §2).
//
// Scanner reads lines of up to 64 MiB and keeps working after a line that is
// too long; Writer writes one JSON value per line, safe for concurrent use.
//
// A typical read loop:
//
//	sc := jsonl.NewScanner(r, 0)
//	for sc.Scan() {
//		if errors.Is(sc.Err(), jsonl.ErrLineTooLong) {
//			// reply with a JSON-RPC error and carry on
//			continue
//		}
//		handle(sc.Bytes())
//	}
//	if err := sc.Err(); err != nil {
//		// the connection broke
//	}
package jsonl

import (
	"bufio"
	"errors"
	"io"
)

// DefaultMaxLine is the longest line a Scanner accepts when max <= 0
// (PROTOCOL §2: both sides accept lines up to 64 MiB).
const DefaultMaxLine = 64 << 20

// ErrLineTooLong is reported for a line longer than the scanner limit.
var ErrLineTooLong = errors.New("jsonl: line too long")

const (
	readerSize = 64 << 10 // bufio.Reader buffer
	keepBuffer = 1 << 20  // larger line buffers are released before the next line
)

// Scanner reads newline-delimited lines from a reader. Unlike bufio.Scanner it
// survives a line that exceeds the limit: the line is discarded, reported once
// as ErrLineTooLong, and scanning continues with the next line.
//
// Scan blocks in Read and cannot take a context; close the reader to unblock it.
type Scanner struct {
	r         *bufio.Reader
	max       int
	buf       []byte
	tok       []byte
	tokErr    error
	discarded int
	err       error
	done      bool
}

// NewScanner returns a Scanner reading from r. Lines longer than max bytes are
// dropped; max <= 0 selects DefaultMaxLine. The limit counts the bytes before
// the '\n', including a trailing '\r'.
func NewScanner(r io.Reader, max int) *Scanner {
	if max <= 0 {
		max = DefaultMaxLine
	}
	return &Scanner{r: bufio.NewReaderSize(r, readerSize), max: max}
}

// Scan advances to the next line. It returns true when a line, or a dropped
// over-long line, is available, and false at the end of input or on a read
// error. Empty lines are returned as empty tokens; empty input yields no token.
func (s *Scanner) Scan() bool {
	s.tok, s.tokErr, s.discarded = nil, nil, 0
	if s.done {
		return false
	}
	if cap(s.buf) > keepBuffer {
		s.buf = nil
	} else {
		s.buf = s.buf[:0]
	}
	tooLong, total := false, 0
	for {
		chunk, err := s.r.ReadSlice('\n')
		data := chunk
		if err == nil {
			data = chunk[:len(chunk)-1] // drop '\n'
		}
		total += len(data)
		if !tooLong {
			if len(s.buf)+len(data) > s.max {
				tooLong = true
				s.buf = nil // release what was collected
			} else {
				s.buf = append(s.buf, data...)
			}
		}
		switch {
		case err == nil:
			return s.emit(tooLong, total)
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			s.done = true
			if total == 0 && !tooLong {
				return false // clean end of input
			}
			return s.emit(tooLong, total) // last line without '\n'
		default:
			s.done, s.err = true, err // read error; a partial line is dropped
			return false
		}
	}
}

func (s *Scanner) emit(tooLong bool, total int) bool {
	if tooLong {
		s.tokErr = ErrLineTooLong
		s.discarded = total
		s.buf = nil
		return true
	}
	if n := len(s.buf); n > 0 && s.buf[n-1] == '\r' {
		s.buf = s.buf[:n-1]
	}
	s.tok = s.buf
	return true
}

// Bytes returns the current line without its '\n' and one trailing '\r'. The
// slice is valid until the next call to Scan. It is nil for a dropped line.
func (s *Scanner) Bytes() []byte { return s.tok }

// Text returns the current line as a string.
func (s *Scanner) Text() string { return string(s.tok) }

// Discarded returns the length in bytes (without the '\n') of the line that
// was dropped as too long, or 0 when the current line was kept.
func (s *Scanner) Discarded() int { return s.discarded }

// Err returns the error of the current token after Scan returned true:
// ErrLineTooLong if the line was dropped, otherwise nil. After Scan returned
// false it returns the terminal read error, which is nil at io.EOF.
func (s *Scanner) Err() error {
	if s.tokErr != nil {
		return s.tokErr
	}
	return s.err
}
