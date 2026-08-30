package jstarget

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// hashHeaderPrefix opens the single leading comment line Filter injects into
// both outputs when there is at least one "@target daemon" region. It is a
// plain "//" line comment, not a JSDoc block, so it can never itself be
// mistaken for a "@target" annotation by Regions().
const hashHeaderPrefix = "// @target-hash sha256:"

// FilterResult is the output of a subtractive @target filter pass over one
// annotated source file: everything "@target daemon" removed (Device), and
// only that removed material, extracted (Daemon). See Filter's doc comment
// for the two artifacts' exact contract.
type FilterResult struct {
	// Device is src with every "@target daemon" region -- construct and its
	// annotating JSDoc comment -- removed. "@target device" and
	// "@target both" regions, and all unannotated code, are left exactly as
	// authored.
	//
	// When there is at least one "@target daemon" region, Device carries
	// one trailing "// @target-hash sha256:<hex>" comment line, appended
	// after all source content, so device and daemon deployments can
	// detect skew (see Hash below). When there are none, Device is
	// byte-identical to src -- no header line is added at all -- which is
	// what lets an entirely unannotated file round-trip byte-for-byte
	// (issue #569's pool-pump.js requirement).
	//
	// Every line number in src is preserved exactly, whether or not it
	// precedes a removal: each removed span is replaced with exactly as
	// many newline characters as it contained, never fewer, and the hash
	// header is appended after everything else rather than prepended, so
	// it never shifts any surviving line. This is a deliberate choice
	// over renumbering, made because stack traces and --no-minify
	// debugging output line numbers that must still point at the
	// authored file.
	Device []byte

	// Daemon is the concatenation of every "@target daemon" region's own
	// source bytes (construct only, annotating comment excluded, matching
	// Region's contract), in source order, each pair separated by a blank
	// line. It carries the same "// @target-hash sha256:<hex>" header as
	// Device when non-empty.
	//
	// Daemon's line numbers do NOT correspond to src's: regions are, in
	// general, thousands of lines apart in the authoring file (that gap is
	// the entire point of moving this code off the device), so padding
	// Daemon out to match would mean a mostly-blank file. Daemon is a new
	// artifact, not a slice of src, and is documented as such.
	//
	// Daemon is nil when src has no "@target daemon" regions -- never a
	// header-only or otherwise non-empty value -- so "no annotations" is
	// distinguishable from "annotated but empty" by a simple len() check.
	Daemon []byte

	// Hash is the sha256 hex digest of the extracted daemon body (the
	// concatenation described above, before the header is added to either
	// output). Empty ("") exactly when Daemon is nil.
	Hash string

	// Regions is every daemon region Filter extracted, in source order, so
	// callers that need diagnostics (which construct, at what offset) do
	// not have to re-parse. This is a subset of what Regions(src) itself
	// would return: only Target == TargetDaemon entries.
	Regions []Region
}

// Filter runs jstarget's parser over src and produces the subtractive
// device/daemon split described in issue #569: Device is src with every
// "@target daemon" region removed; Daemon is the concatenation of exactly
// those regions. Unannotated code -- the implicit "both" default -- is left
// untouched in Device and never appears in Daemon.
//
// Filter is subtractive only: it never generates code into Device, and
// never reintroduces this repo's "// >>> GENERATED: ... >>>" marker
// convention (rejected during #544's design, see that issue). The returned
// Device is always the thing a human reads, edits, uploads with
// --no-minify, and debugs -- minus daemon-only material, nothing added
// beyond the one optional hash header line documented on FilterResult.
//
// Filter must run before minification: it depends on the JSDoc comments
// that carry "@target" annotations, which a minifier is free to strip or
// mangle. See pkg/shelly/script's doUpload, which minifies immediately
// before splitting a script for upload -- Filter's output is meant to feed
// that same minify step, not follow it.
func Filter(src []byte) (FilterResult, error) {
	ast, comments, err := parseSource(src)
	if err != nil {
		return FilterResult{}, err
	}
	resolved, err := resolveWithComments(src, ast, comments)
	if err != nil {
		return FilterResult{}, err
	}

	var daemon []Region
	var rawCuts []cutSpan
	for _, r := range resolved {
		if r.Target != TargetDaemon {
			continue
		}
		daemon = append(daemon, r.Region)
		start, end := extendForListSeparator(src, r.commentStart, r.End)
		start = trimLeadingIndent(src, start)
		rawCuts = append(rawCuts, cutSpan{start: start, end: end})
	}

	if len(daemon) == 0 {
		return FilterResult{Device: append([]byte(nil), src...)}, nil
	}

	body := daemonBody(src, daemon)
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	header := hashHeaderPrefix + hash + "\n"

	// The header is appended, not prepended: every surviving line number in
	// src stays exactly what it was, at the cost of the header itself
	// having no fixed line number. Compare Daemon below, where the header
	// leads instead -- Daemon has no src line numbers to preserve, so
	// putting the header first there is simply more readable.
	device := removeCuts(src, mergeCuts(rawCuts))
	if len(device) > 0 && device[len(device)-1] != '\n' {
		device = append(device, '\n')
	}
	device = append(device, []byte(header)...)

	daemonOut := append([]byte(header+"\n"), body...)

	return FilterResult{
		Device:  device,
		Daemon:  daemonOut,
		Hash:    hash,
		Regions: daemon,
	}, nil
}

// daemonBody concatenates each region's own source bytes (construct only,
// JSDoc excluded) in source order, blank-line separated. regions must
// already be in source order, which resolveWithComments guarantees.
func daemonBody(src []byte, regions []Region) []byte {
	var buf bytes.Buffer
	for i, r := range regions {
		if i > 0 {
			buf.WriteString("\n\n")
		}
		buf.Write(src[r.Start:r.End])
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}

// cutSpan is a byte range to remove from Device.
type cutSpan struct{ start, end int }

// extendForListSeparator grows [start,end) to also consume a comma
// separating this construct from a sibling in the same list -- an object
// literal's property list is the only candidate kind this applies to;
// Program/BlockStatement statements are never comma-separated, so this is a
// no-op for them.
//
// It prefers the trailing comma (the construct is not last in its list): if
// one immediately follows end, modulo whitespace, it is absorbed. Only when
// no trailing comma exists (the construct is last, or the only element) does
// it fall back to absorbing a leading comma before start. This ordering
// matters when two adjacent list elements are both removed: the earlier
// element's forward extension claims the comma between them, so the later
// element's backward search would otherwise re-claim the same byte -- see
// mergeCuts, which resolves that overlap by union rather than by trying to
// make each region's extension aware of its neighbours.
func extendForListSeparator(src []byte, start, end int) (int, int) {
	j := end
	for j < len(src) && isSpace(src[j]) {
		j++
	}
	if j < len(src) && src[j] == ',' {
		return start, j + 1
	}

	i := start
	for i > 0 && isSpace(src[i-1]) {
		i--
	}
	if i > 0 && src[i-1] == ',' {
		return i - 1, end
	}

	return start, end
}

// trimLeadingIndent extends start backward over a run of pure horizontal
// whitespace (spaces and tabs only -- never a newline), so a cut does not
// leave a whitespace-only "indentation" line behind it when its
// construct's own start already sits at the beginning of its content on
// that line. This is purely cosmetic: removeCuts's newline-counting
// guarantee holds regardless, since spaces and tabs contribute no
// newlines to count either way.
func trimLeadingIndent(src []byte, start int) int {
	i := start
	for i > 0 && (src[i-1] == ' ' || src[i-1] == '\t') {
		i--
	}
	return i
}

// mergeCuts sorts and coalesces overlapping or touching cut spans into the
// minimal set of disjoint spans that cover the same bytes. Two independently
// computed cuts can overlap by exactly the shared list-separator comma
// between adjacent removed properties (see extendForListSeparator); merging
// is what makes that safe to apply without special-casing it there.
func mergeCuts(cuts []cutSpan) []cutSpan {
	if len(cuts) == 0 {
		return nil
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].start < cuts[j].start })

	merged := []cutSpan{cuts[0]}
	for _, c := range cuts[1:] {
		last := &merged[len(merged)-1]
		if c.start > last.end {
			merged = append(merged, c)
			continue
		}
		if c.end > last.end {
			last.end = c.end
		}
	}
	return merged
}

// removeCuts returns src with every span in cuts (sorted, disjoint --
// mergeCuts's contract) deleted, each replaced by as many newline
// characters as it originally contained. That keeps every line number
// outside a cut identical to its number in src; see FilterResult.Device.
func removeCuts(src []byte, cuts []cutSpan) []byte {
	var out bytes.Buffer
	out.Grow(len(src))
	pos := 0
	for _, c := range cuts {
		out.Write(src[pos:c.start])
		out.Write(bytes.Repeat([]byte{'\n'}, bytes.Count(src[c.start:c.end], []byte{'\n'})))
		pos = c.end
	}
	out.Write(src[pos:])
	return out.Bytes()
}

// ParseHashHeader extracts the sha256 hex digest from a "// @target-hash
// sha256:<hex>" header line, as embedded by Filter into both Device and
// Daemon. It returns ("", false) if line does not carry that header, so
// callers can distinguish "no header" (unannotated file) from "header
// present". This is the read side of the deployment-skew check described in
// #544 §10 and #569: compare the hash a running device script reports
// against the hash the daemon-side extraction currently expects.
func ParseHashHeader(line string) (hash string, ok bool) {
	if len(line) <= len(hashHeaderPrefix) || line[:len(hashHeaderPrefix)] != hashHeaderPrefix {
		return "", false
	}
	return line[len(hashHeaderPrefix):], true
}
