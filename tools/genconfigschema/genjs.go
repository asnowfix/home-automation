package main

import (
	"fmt"
	"strconv"
	"strings"
)

// marker returns the begin/end comment lines that delimit a generated block
// of a given tag (e.g. "CONFIG_SCHEMA") inside a .js file. Everything between
// them is replaced verbatim on each run; everything outside is left alone,
// so the rest of the script stays a normal, hand-edited file.
func marker(tag string) (begin, end string) {
	begin = fmt.Sprintf("// >>> GENERATED: %s (source: schema JSON; regenerate via `make generate` — DO NOT EDIT BY HAND) >>>", tag)
	end = fmt.Sprintf("// <<< GENERATED: %s <<<", tag)
	return begin, end
}

// InjectBlock replaces the content between the begin/end markers for tag in
// src with newBody (which must not itself contain the markers), preserving
// everything else in the file. It fails loudly if the markers are missing or
// malformed, rather than silently appending — a missing marker means the .js
// file needs a one-time hand-edit to add the delimiter comments.
func InjectBlock(src, tag, newBody string) (string, error) {
	begin, end := marker(tag)
	startIdx := strings.Index(src, begin)
	if startIdx == -1 {
		return "", fmt.Errorf("marker %q not found; add it (with a matching %q) around the block to generate", begin, end)
	}
	afterBegin := startIdx + len(begin)
	endIdx := strings.Index(src[afterBegin:], end)
	if endIdx == -1 {
		return "", fmt.Errorf("marker %q not found after its matching begin marker", end)
	}
	endIdx += afterBegin

	var out strings.Builder
	out.WriteString(src[:afterBegin])
	out.WriteString("\n")
	out.WriteString(newBody)
	out.WriteString(end)
	out.WriteString(src[endIdx+len(end):])
	return out.String(), nil
}

// GenerateJSConfigSchema renders the CONFIG_SCHEMA object literal body (the
// part between the markers) for schema.Fields. Descriptions are emitted as
// `//` comments immediately above each field — stripped by the minifier —
// never as an object property, so they cost zero device heap (#439/#433).
func GenerateJSConfigSchema(schema *Schema) (string, error) {
	var b strings.Builder
	b.WriteString("var CONFIG_SCHEMA = {\n")
	for i, f := range schema.Fields {
		if f.Description != "" {
			fmt.Fprintf(&b, "  // %s\n", f.Description)
		}
		fmt.Fprintf(&b, "  %s: {\n", f.Name)
		fmt.Fprintf(&b, "    key: %s,\n", strconv.Quote(f.Key))
		lit, err := jsLiteral(f)
		if err != nil {
			return "", fmt.Errorf("field %q: %w", f.Name, err)
		}
		fmt.Fprintf(&b, "    default: %s,\n", lit)
		fmt.Fprintf(&b, "    type: %s", strconv.Quote(f.Type))
		if f.CliOnly {
			b.WriteString(",\n    cliOnly: true")
		}
		if f.Required {
			b.WriteString(",\n    required: true")
		}
		b.WriteString("\n  }")
		if i < len(schema.Fields)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("};\n")
	return b.String(), nil
}

// GenerateJSZoneKeySpecs renders garden.js's ZONE_KEY_SPECS array literal
// body from schema.ZoneFields.
func GenerateJSZoneKeySpecs(schema *Schema) (string, error) {
	var b strings.Builder
	b.WriteString("var ZONE_KEY_SPECS = [\n")
	for i, zf := range schema.ZoneFields {
		fmt.Fprintf(&b, "  {field: %s, key: %s, type: %s}", strconv.Quote(zf.Field), strconv.Quote(zf.Key), strconv.Quote(zf.Type))
		if i < len(schema.ZoneFields)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("];\n")
	return b.String(), nil
}

// jsLiteral renders f.Default as a JS literal matching f.Type.
func jsLiteral(f Field) (string, error) {
	switch v := f.Default.(type) {
	case bool:
		if f.Type != "boolean" {
			return "", fmt.Errorf("default %v is bool but type is %q", v, f.Type)
		}
		return strconv.FormatBool(v), nil
	case float64:
		if f.Type != "number" {
			return "", fmt.Errorf("default %v is number but type is %q", v, f.Type)
		}
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), nil
		}
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case string:
		if f.Type != "string" {
			return "", fmt.Errorf("default %q is string but type is %q", v, f.Type)
		}
		return strconv.Quote(v), nil
	case nil:
		return "null", nil
	default:
		return "", fmt.Errorf("unsupported default value type %T", v)
	}
}
