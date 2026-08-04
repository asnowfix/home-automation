package script_test

// This file lives in an external test package (script_test, not script) so
// it can import internal/shelly/scripts without creating an import cycle:
// internal/shelly/scripts already imports pkg/shelly/script.

import (
	"io/fs"
	"testing"
	"unicode/utf8"

	"github.com/asnowfix/home-automation/internal/shelly/scripts"
	"github.com/asnowfix/home-automation/pkg/shelly/script"
)

// TestSplitChunks_PoolPumpEmbedded is a regression test for issue #423: it
// chunks the real, unminified internal/shelly/scripts/pool-pump.js (read
// from the embedded FS, the same source used by real uploads) and asserts
// every chunk is individually valid UTF-8. This keeps the bug from
// silently returning as the script's comments (which contain non-ASCII
// punctuation like em dashes and ellipses) change over time.
func TestSplitChunks_PoolPumpEmbedded(t *testing.T) {
	buf, err := fs.ReadFile(scripts.GetFS(), "pool-pump.js")
	if err != nil {
		t.Fatalf("failed to read embedded pool-pump.js: %v", err)
	}
	if len(buf) == 0 {
		t.Fatal("embedded pool-pump.js is empty")
	}

	const chunkSize = 2048
	chunks := script.SplitChunks(buf, chunkSize)

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	var recombined []byte
	for i, chunk := range chunks {
		if len(chunk) == 0 {
			t.Errorf("chunk %d is empty", i)
		}
		if !utf8.Valid(chunk) {
			t.Errorf("chunk %d is not valid UTF-8 (len=%d)", i, len(chunk))
		}
		recombined = append(recombined, chunk...)
	}

	if string(recombined) != string(buf) {
		t.Errorf("recombined chunks (%d bytes) do not reproduce pool-pump.js (%d bytes) byte-for-byte", len(recombined), len(buf))
	}
}
