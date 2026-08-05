package script

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// SymbolMap is a flat mangled -> original name table for a script's
// top-level symbols, persisted alongside the minified script.
//
// It exists BECAUSE a Source Map v3 alone does not solve the actual
// problem: Shelly crash traces give function names and code snippets with
// no line/column information at all, e.g.
//
//	in function "storeValue" called from storeValue("turnover-today",computeTurnoverToday(t))
//
// Source maps resolve generated (line, column) -> original (line, column);
// we don't have a (line, column) to look up, only bare identifier text. So
// SymbolMap instead gives a direct string substitution table: every
// top-level identifier that MangleTopLevel could have renamed, mapped back
// to its original name. Demangle uses it to turn a crash message with
// mangled names back into one a human can read.
//
// Limitations (see Demangle):
//   - Only TOP-LEVEL symbols are covered. Local/nested identifiers are
//     deliberately excluded because their mangled names are reused across
//     different scopes (not unique), so a flat table for them would be
//     wrong, not just incomplete.
//   - It is a textual, word-boundary substitution over the whole message,
//     not a real parser -- a mangled name that happens to appear inside a
//     JSON string value in the crash message (e.g. a KVS key literally
//     equal to a mangled identifier) would also get rewritten. Accepted
//     trade-off for a diagnostic tool.
type SymbolMap struct {
	// Script is the originating script's filename, e.g. "pool-pump.js".
	Script string `json:"script"`
	// Engine that produced this map (always EngineEsbuild today).
	Engine Engine `json:"engine"`
	// Symbols maps mangled identifier -> original identifier.
	Symbols map[string]string `json:"symbols"`
}

// WriteSymbolMap persists m as indented JSON at path, creating parent
// directories as needed.
func WriteSymbolMap(path string, m *SymbolMap) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create symbol map directory: %w", err)
	}
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal symbol map: %w", err)
	}
	buf = append(buf, '\n')
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return fmt.Errorf("write symbol map %s: %w", path, err)
	}
	return nil
}

// ReadSymbolMap loads a SymbolMap previously written by WriteSymbolMap.
func ReadSymbolMap(path string) (*SymbolMap, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read symbol map %s: %w", path, err)
	}
	var m SymbolMap
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, fmt.Errorf("parse symbol map %s: %w", path, err)
	}
	return &m, nil
}

// Demangle rewrites every whole-identifier occurrence of a mangled name in
// msg with its original name, using m's table. Longer mangled names are
// substituted first so that (in the rare case one mangled name is a prefix
// of another -- it isn't for esbuild's single-char-then-two-char scheme,
// but this is defensive) a shorter name doesn't shadow a longer match
// first. Matching requires an identifier boundary on both sides (so "a"
// only matches a standalone "a" token, never the "a" inside "abc" or
// "_a1"). A nil receiver or an empty table is a safe no-op that returns msg
// unchanged.
func (m *SymbolMap) Demangle(msg string) string {
	if m == nil || len(m.Symbols) == 0 {
		return msg
	}

	keys := make([]string, 0, len(m.Symbols))
	for k := range m.Symbols {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })

	out := make([]byte, 0, len(msg))
	i := 0
	for i < len(msg) {
		if isIdentStart(msg[i]) && (i == 0 || !isIdentPart(msg[i-1])) {
			matched := false
			for _, k := range keys {
				if len(k) == 0 || i+len(k) > len(msg) {
					continue
				}
				if msg[i:i+len(k)] != k {
					continue
				}
				end := i + len(k)
				if end < len(msg) && isIdentPart(msg[end]) {
					continue // not a whole-identifier match
				}
				out = append(out, m.Symbols[k]...)
				i = end
				matched = true
				break
			}
			if matched {
				continue
			}
		}
		out = append(out, msg[i])
		i++
	}
	return string(out)
}

// mapping is one decoded segment of a Source Map v3 "mappings" field,
// keeping only what buildSymbolMap needs: the generated position and
// (optionally) an index into the map's "names" array.
type mapping struct {
	genLine int
	genCol  int
	nameIdx int // -1 if this segment carries no name
}

// vlqBase64Alphabet is the standard Base64 alphabet used by the Source Map
// v3 "Base64 VLQ" encoding (https://sourcemaps.info/spec.html and the
// original vlq.js this format was borrowed from). It is NOT the same thing
// as ordinary base64 encoding of bytes -- each character encodes a 5-bit
// group of a variable-length, zig-zag-signed integer, with the 6th bit as a
// continuation flag.
const vlqBase64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

var vlqDecodeTable = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i, c := range vlqBase64Alphabet {
		t[byte(c)] = int8(i)
	}
	return t
}()

// decodeMappings decodes a Source Map v3 "mappings" field into a flat list
// of segments. Only the generated line/column and name index are tracked
// (source index/line/column deltas are consumed to keep the decoder state
// machine correct, per spec, but discarded -- buildSymbolMap doesn't need
// them: it already knows which ORIGINAL names it cares about from
// topLevelDeclarations, not from the source map's own line/column
// information).
func decodeMappings(mappings string) ([]mapping, error) {
	var segs []mapping

	genLine, genCol := 0, 0
	srcIdx, srcLine, srcCol, nameIdx := 0, 0, 0, 0

	i := 0
	n := len(mappings)

	readVLQ := func() (int, error) {
		val := 0
		shift := uint(0)
		for {
			if i >= n {
				return 0, fmt.Errorf("truncated VLQ at offset %d", i)
			}
			c := mappings[i]
			i++
			d := vlqDecodeTable[c]
			if d < 0 {
				return 0, fmt.Errorf("invalid base64-vlq byte %q at offset %d", c, i-1)
			}
			cont := d & 0x20
			digit := int(d & 0x1f)
			val += digit << shift
			shift += 5
			if cont == 0 {
				break
			}
		}
		if val&1 != 0 {
			return -(val >> 1), nil
		}
		return val >> 1, nil
	}

	for i < n {
		switch mappings[i] {
		case ';':
			genLine++
			genCol = 0
			i++
			continue
		case ',':
			i++
			continue
		}

		dCol, err := readVLQ()
		if err != nil {
			return nil, err
		}
		genCol += dCol

		seg := mapping{genLine: genLine, genCol: genCol, nameIdx: -1}

		if i >= n || mappings[i] == ',' || mappings[i] == ';' {
			// Generated-code-only segment (no source mapping). Valid but
			// irrelevant to us since it can't carry a name.
			segs = append(segs, seg)
			continue
		}

		dSrc, err := readVLQ()
		if err != nil {
			return nil, err
		}
		srcIdx += dSrc

		dSrcLine, err := readVLQ()
		if err != nil {
			return nil, err
		}
		srcLine += dSrcLine

		dSrcCol, err := readVLQ()
		if err != nil {
			return nil, err
		}
		srcCol += dSrcCol
		_ = srcIdx // consumed for decoder-state correctness only

		if i < n && mappings[i] != ',' && mappings[i] != ';' {
			dName, err := readVLQ()
			if err != nil {
				return nil, err
			}
			nameIdx += dName
			seg.nameIdx = nameIdx
		}

		segs = append(segs, seg)
	}

	return segs, nil
}
