package script

import "unicode/utf8"

// SplitChunks splits buf into chunks of at most max bytes each, for use by
// the script-upload RPC loop (each chunk becomes one PutCode call, glued
// back together on the device via Append: true).
//
// Chunks are cut on UTF-8 rune boundaries, never in the middle of a
// multi-byte rune: when the prospective end-of-chunk byte offset lands on a
// UTF-8 continuation byte, the boundary is walked back to the start of that
// rune, so the whole rune moves into the next chunk instead. Chunks then
// come out slightly smaller than max, which is harmless — the device
// imposes no fixed chunk size, and Append: true stitches them back
// together (see issue #423: byte-oriented slicing bisected a multi-byte
// rune, producing a chunk that was not valid UTF-8 on its own; the device
// then rejected the RPC call with "Missing or bad argument 'code'!").
//
// Defensively guards against a rune wider than max (impossible for
// well-formed UTF-8 with any realistic max, since the widest UTF-8 rune is
// 4 bytes, but guarded anyway): rather than loop forever or emit a
// zero-length chunk, such a rune is allowed to be split.
func SplitChunks(buf []byte, max int) [][]byte {
	if max <= 0 {
		panic("SplitChunks: max must be positive")
	}

	var chunks [][]byte
	for i := 0; i < len(buf); {
		end := i + max
		if end >= len(buf) {
			end = len(buf)
		} else {
			// end < len(buf): don't split a multi-byte rune across chunks.
			// Walk end back to the start of the rune it landed inside, but
			// never below i+1 — that would produce a zero-length chunk and
			// make no progress (only possible if a single rune is wider
			// than max, which cannot happen with valid UTF-8 in practice).
			for end > i+1 && !utf8.RuneStart(buf[end]) {
				end--
			}
		}
		chunks = append(chunks, buf[i:end])
		i = end
	}
	return chunks
}
