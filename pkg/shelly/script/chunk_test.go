package script

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// assertChunksValid asserts that every chunk produced by SplitChunks is
// itself valid UTF-8, and that concatenating them reproduces buf exactly.
func assertChunksValid(t *testing.T, buf []byte, max int, chunks [][]byte) {
	t.Helper()

	var recombined []byte
	for i, chunk := range chunks {
		if len(chunk) == 0 {
			t.Errorf("chunk %d is empty", i)
		}
		if !utf8.Valid(chunk) {
			t.Errorf("chunk %d is not valid UTF-8: %q", i, chunk)
		}
		recombined = append(recombined, chunk...)
	}

	if !bytes.Equal(recombined, buf) {
		t.Errorf("recombined chunks do not reproduce input byte-for-byte\nwant: %q\ngot:  %q", buf, recombined)
	}
}

// TestSplitChunks_MultiByteCharStraddlesBoundary reproduces the exact defect
// from issue #423: a multi-byte UTF-8 character positioned so that a naive
// byte-offset boundary at `max` falls inside it. This mirrors the real
// pool-pump.js failure, where a U+2026 (HORIZONTAL ELLIPSIS, 3 bytes:
// 0xE2 0x80 0xA6) straddled a 2048-byte boundary.
func TestSplitChunks_MultiByteCharStraddlesBoundary(t *testing.T) {
	// "ABCDEFG" (7 bytes) + "…" (U+2026, 3 bytes: indices 7,8,9) + "HIJKLMN".
	// With max=8, a naive slice buf[0:8] would end at index 8, which is the
	// *second* byte of the 3-byte ellipsis (a UTF-8 continuation byte) —
	// exactly the bug: it bisects the rune.
	buf := []byte("ABCDEFG" + "…" + "HIJKLMN")
	const max = 8

	// Sanity-check the fixture actually reproduces the hazard before
	// asserting anything about SplitChunks: byte 8 must be a UTF-8
	// continuation byte (0x80-0xBF), i.e. NOT a rune start.
	if utf8.RuneStart(buf[max]) {
		t.Fatalf("test fixture is broken: byte %d is not a continuation byte (%#x)", max, buf[max])
	}

	chunks := SplitChunks(buf, max)
	assertChunksValid(t, buf, max, chunks)
}

// TestSplitChunks_TableDriven covers the edge cases called out in #423's
// test plan: empty input, input shorter than one chunk, input exactly one
// chunk long, and a multi-byte character at the very last byte.
func TestSplitChunks_TableDriven(t *testing.T) {
	tests := []struct {
		name       string
		buf        []byte
		max        int
		wantChunks int // -1 means "don't check exact count"
	}{
		{
			name:       "empty input",
			buf:        []byte{},
			max:        2048,
			wantChunks: 0,
		},
		{
			name:       "input shorter than one chunk",
			buf:        []byte("hello"),
			max:        2048,
			wantChunks: 1,
		},
		{
			name:       "input exactly one chunk long",
			buf:        bytes.Repeat([]byte("x"), 8),
			max:        8,
			wantChunks: 1,
		},
		{
			name:       "multi-byte char at the very last byte",
			buf:        []byte(strings.Repeat("A", 7) + "é"), // 7 ASCII bytes + 2-byte 'é'
			max:        8,                                    // boundary at byte 8 lands inside 'é' (index 7,8)
			wantChunks: -1,
		},
		{
			name:       "multi-byte char straddling boundary (ellipsis)",
			buf:        []byte("ABCDEFG" + "…" + "HIJKLMN"),
			max:        8,
			wantChunks: -1,
		},
		{
			name:       "pure ASCII, many chunks",
			buf:        bytes.Repeat([]byte("z"), 5000),
			max:        2048,
			wantChunks: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := SplitChunks(tt.buf, tt.max)

			if tt.wantChunks >= 0 && len(chunks) != tt.wantChunks {
				t.Errorf("got %d chunks, want %d", len(chunks), tt.wantChunks)
			}

			if len(tt.buf) == 0 {
				if len(chunks) != 0 {
					t.Errorf("expected no chunks for empty input, got %d", len(chunks))
				}
				return
			}

			assertChunksValid(t, tt.buf, tt.max, chunks)

			// No chunk should exceed max... except when a single rune is
			// pathologically wider than max (not exercised by these cases),
			// so also assert the specific expectation for the sane cases
			// here: every chunk length is <= max.
			for i, c := range chunks {
				if len(c) > tt.max {
					t.Errorf("chunk %d has length %d, exceeds max %d", i, len(c), tt.max)
				}
			}
		})
	}
}

// TestSplitChunks_SingleRuneWiderThanMax guards against the pathological
// case called out in #423: a single character larger than the chunk size
// must not cause an infinite loop or a zero-length chunk.
func TestSplitChunks_SingleRuneWiderThanMax(t *testing.T) {
	// U+1F600 GRINNING FACE is 4 bytes in UTF-8; max=1 forces the walk-back
	// to have nowhere valid to land.
	buf := []byte("\U0001F600")
	const max = 1

	done := make(chan [][]byte, 1)
	go func() {
		done <- SplitChunks(buf, max)
	}()

	select {
	case chunks := <-done:
		if len(chunks) == 0 {
			t.Fatal("expected at least one chunk")
		}
		for i, c := range chunks {
			if len(c) == 0 {
				t.Errorf("chunk %d is zero-length", i)
			}
		}
		var recombined []byte
		for _, c := range chunks {
			recombined = append(recombined, c...)
		}
		if !bytes.Equal(recombined, buf) {
			t.Errorf("recombined chunks do not reproduce input: want %q got %q", buf, recombined)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SplitChunks did not return: suspected infinite loop")
	}
}
