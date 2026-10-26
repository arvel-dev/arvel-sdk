// SPDX-License-Identifier: Apache-2.0

package jsonl

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

// collect runs the scan loop and records each token's text and error.
func collect(sc *Scanner) (tokens []string, errs []error) {
	for sc.Scan() {
		tokens = append(tokens, sc.Text())
		errs = append(errs, sc.Err())
	}
	return tokens, errs
}

// repeatReader yields n bytes of 'x' followed by suffix without allocating n bytes.
type repeatReader struct {
	n      int
	suffix string
	pos    int
}

func (r *repeatReader) Read(p []byte) (int, error) {
	total := r.n + len(r.suffix)
	if r.pos >= total {
		return 0, io.EOF
	}
	n := 0
	for n < len(p) && r.pos < total {
		if r.pos < r.n {
			p[n] = 'x'
		} else {
			p[n] = r.suffix[r.pos-r.n]
		}
		n++
		r.pos++
	}
	return n, nil
}

func TestScanner_Lines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"two lines", "a\nb\n", []string{"a", "b"}},
		{"no trailing newline", "a\nb", []string{"a", "b"}},
		{"empty line kept", "a\n\nb\n", []string{"a", "", "b"}},
		{"crlf stripped", "a\r\nb\r\n", []string{"a", "b"}},
		{"empty input", "", nil},
		{"only newline", "\n", []string{""}},
		{"inner cr kept", "a\rb\n", []string{"a\rb"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sc := NewScanner(strings.NewReader(c.in), 100)
			got, errs := collect(sc)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("tokens = %q, want %q", got, c.want)
			}
			for _, err := range errs {
				if err != nil {
					t.Fatalf("unexpected token error %v", err)
				}
			}
			if err := sc.Err(); err != nil {
				t.Fatalf("terminal Err = %v", err)
			}
		})
	}
}

func TestScanner_KeepsWorkingAfterLongLine(t *testing.T) {
	in := "short\n" + strings.Repeat("x", 100) + "\nafter\n"
	sc := NewScanner(strings.NewReader(in), 10)

	if !sc.Scan() || sc.Text() != "short" || sc.Err() != nil {
		t.Fatalf("first token = %q err=%v", sc.Text(), sc.Err())
	}
	if !sc.Scan() {
		t.Fatal("expected a token for the long line")
	}
	if !errors.Is(sc.Err(), ErrLineTooLong) || sc.Discarded() != 100 || sc.Bytes() != nil {
		t.Fatalf("long line: err=%v discarded=%d bytes=%v", sc.Err(), sc.Discarded(), sc.Bytes())
	}
	if !sc.Scan() || sc.Text() != "after" || sc.Err() != nil || sc.Discarded() != 0 {
		t.Fatalf("third token = %q err=%v discarded=%d", sc.Text(), sc.Err(), sc.Discarded())
	}
	if sc.Scan() || sc.Err() != nil {
		t.Fatalf("expected clean end, err=%v", sc.Err())
	}
}

func TestScanner_LongLineSpansReaderBuffer(t *testing.T) {
	in := "a\n" + strings.Repeat("y", 300000) + "\nb\n"
	sc := NewScanner(strings.NewReader(in), 1000)
	got, errs := collect(sc)
	if !reflect.DeepEqual(got, []string{"a", "", "b"}) {
		t.Fatalf("tokens = %d entries: %q", len(got), got)
	}
	if !errors.Is(errs[1], ErrLineTooLong) || errs[0] != nil || errs[2] != nil {
		t.Fatalf("errs = %v", errs)
	}
}

func TestScanner_DiscardedCount(t *testing.T) {
	in := "a\n" + strings.Repeat("y", 300000) + "\nb\n"
	sc := NewScanner(strings.NewReader(in), 1000)
	sc.Scan()
	sc.Scan()
	if sc.Discarded() != 300000 {
		t.Fatalf("Discarded = %d", sc.Discarded())
	}
}

func TestScanner_ExactLimit(t *testing.T) {
	ok := strings.Repeat("z", 20)
	sc := NewScanner(strings.NewReader(ok+"\n"), 20)
	if !sc.Scan() || sc.Err() != nil || sc.Text() != ok {
		t.Fatalf("exact-limit line rejected: err=%v", sc.Err())
	}
	sc = NewScanner(strings.NewReader(ok+"z\n"), 20)
	if !sc.Scan() || !errors.Is(sc.Err(), ErrLineTooLong) {
		t.Fatalf("limit+1 line accepted: err=%v", sc.Err())
	}
}

func TestScanner_LongLastLineWithoutNewline(t *testing.T) {
	sc := NewScanner(strings.NewReader("ok\n"+strings.Repeat("q", 50)), 10)
	got, errs := collect(sc)
	if len(got) != 2 || errs[0] != nil || !errors.Is(errs[1], ErrLineTooLong) {
		t.Fatalf("tokens=%q errs=%v", got, errs)
	}
	if sc.Err() != nil {
		t.Fatalf("terminal Err = %v", sc.Err())
	}
}

func TestScanner_LineLargerThanReaderBufferWithinLimit(t *testing.T) {
	line := strings.Repeat("a", 100) + strings.Repeat("m", 200<<10) + strings.Repeat("b", 100)
	sc := NewScanner(strings.NewReader(line+"\n"), 1<<20)
	if !sc.Scan() || sc.Err() != nil {
		t.Fatalf("scan failed: %v", sc.Err())
	}
	if got := sc.Text(); got != line {
		t.Fatalf("line corrupted: len=%d want %d", len(got), len(line))
	}
}

func TestScanner_ReadError(t *testing.T) {
	boom := errors.New("boom")
	r := io.MultiReader(strings.NewReader("a\n"), iotest.ErrReader(boom))
	sc := NewScanner(r, 100)
	if !sc.Scan() || sc.Text() != "a" {
		t.Fatal("expected first line")
	}
	if sc.Scan() {
		t.Fatal("expected scan to stop")
	}
	if !errors.Is(sc.Err(), boom) {
		t.Fatalf("Err = %v", sc.Err())
	}
	if sc.Scan() {
		t.Fatal("scan resumed after error")
	}
}

func TestScanner_OneByteReader(t *testing.T) {
	in := "alpha\nbeta\r\n\ngamma"
	want, _ := collect(NewScanner(strings.NewReader(in), 100))
	got, _ := collect(NewScanner(iotest.OneByteReader(strings.NewReader(in)), 100))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("one-byte reader: %q, want %q", got, want)
	}
}

func TestScanner_DefaultMaxLine(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates 64 MiB")
	}
	for _, max := range []int{0, -1} {
		sc := NewScanner(&repeatReader{n: DefaultMaxLine, suffix: "\nnext\n"}, max)
		if !sc.Scan() || sc.Err() != nil || len(sc.Bytes()) != DefaultMaxLine {
			t.Fatalf("max=%d: 64 MiB line rejected: err=%v len=%d", max, sc.Err(), len(sc.Bytes()))
		}
		if !sc.Scan() || sc.Text() != "next" {
			t.Fatalf("max=%d: next line lost", max)
		}
	}
	sc := NewScanner(&repeatReader{n: DefaultMaxLine + 1, suffix: "\nnext\n"}, 0)
	if !sc.Scan() || !errors.Is(sc.Err(), ErrLineTooLong) || sc.Discarded() != DefaultMaxLine+1 {
		t.Fatalf("64 MiB + 1 accepted: err=%v", sc.Err())
	}
	if !sc.Scan() || sc.Text() != "next" {
		t.Fatal("scanner did not survive the long line")
	}
}

func TestScanner_ReleasesLargeBuffer(t *testing.T) {
	in := strings.Repeat("l", 2*keepBuffer) + "\nshort\n"
	sc := NewScanner(strings.NewReader(in), 4*keepBuffer)
	if !sc.Scan() || len(sc.Bytes()) != 2*keepBuffer {
		t.Fatal("large line not read")
	}
	if !sc.Scan() || sc.Text() != "short" {
		t.Fatal("short line lost")
	}
	if cap(sc.buf) > keepBuffer {
		t.Fatalf("buffer not released: cap=%d", cap(sc.buf))
	}
}

func FuzzScanner(f *testing.F) {
	f.Add([]byte("a\nb\n"), uint8(5))
	f.Add([]byte("\n\n"), uint8(1))
	f.Add([]byte("x"), uint8(0))
	f.Add([]byte("\r\n"), uint8(3))
	f.Add(append(bytes.Repeat([]byte("a"), 200), '\n'), uint8(20))
	f.Fuzz(func(t *testing.T, data []byte, limit uint8) {
		max := int(limit)%50 + 1
		parts := bytes.Split(data, []byte("\n"))
		if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
			parts = parts[:len(parts)-1]
		}
		type tok struct {
			text    string
			tooLong bool
		}
		var want []tok
		for _, p := range parts {
			if len(p) > max {
				want = append(want, tok{tooLong: true})
				continue
			}
			p = bytes.TrimSuffix(p, []byte("\r"))
			want = append(want, tok{text: string(p)})
		}
		sc := NewScanner(bytes.NewReader(data), max)
		var got []tok
		for sc.Scan() {
			if errors.Is(sc.Err(), ErrLineTooLong) {
				got = append(got, tok{tooLong: true})
				continue
			}
			got = append(got, tok{text: sc.Text()})
		}
		if sc.Err() != nil {
			t.Fatalf("terminal error %v", sc.Err())
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v (data %q, max %d)", got, want, data, max)
		}
	})
}
