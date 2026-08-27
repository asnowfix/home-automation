package jstarget

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// candidate is a source construct a "@target" annotation can attach to:
// a statement in a Program/BlockStatement body (covers function
// declarations, function expressions assigned to a var, and var
// declarations), or a Property inside an ObjectExpression (covers object
// properties and multi-line object literals, since a Property's own byte
// range already spans a multi-line value).
type candidate struct {
	start, end int
	name       string
}

// targetTagRE extracts the value of a "@target X" JSDoc tag. It intentionally
// does not anchor to the start of the comment: JSDoc content may carry
// leading "*" continuation markers and other tags.
var targetTagRE = regexp.MustCompile(`@target\s+(\S+)`)

// resolve matches every JSDoc "@target" comment against the AST's
// candidates and returns the resolved, validated Regions in source order.
func resolve(src []byte, ast map[string]any, comments []comment) ([]Region, error) {
	candidates := collectCandidates(ast)
	byStart := make(map[int]candidate, len(candidates))
	for _, c := range candidates {
		byStart[c.start] = c
	}

	var regions []Region
	for _, cm := range comments {
		if cm.Type != "Block" || !strings.HasPrefix(cm.Value, "*") {
			// Not a JSDoc block: acorn's onComment strips the "/*"/"*/"
			// delimiters, so a "/** ... */" comment's Value always starts
			// with the extra leading "*". A plain "/* ... */" or "//" line
			// comment cannot carry a "@target" annotation.
			continue
		}
		m := targetTagRE.FindStringSubmatch(cm.Value)
		if m == nil {
			continue
		}

		raw := m[1]
		target, ok := validTargets[raw]
		if !ok {
			return nil, fmt.Errorf("jstarget: unrecognised @target value %q (byte offset %d)", raw, cm.Start)
		}

		next := skipTrivia(src, cm.End, comments)
		c, ok := byStart[next]
		if !ok {
			return nil, fmt.Errorf("jstarget: @target %s annotation (byte offset %d) does not annotate a recognised construct", raw, cm.Start)
		}

		regions = append(regions, Region{Target: target, Start: c.start, End: c.end, Name: c.name})
	}

	sort.Slice(regions, func(i, j int) bool { return regions[i].Start < regions[j].Start })

	if err := checkNesting(regions); err != nil {
		return nil, err
	}

	return regions, nil
}

// skipTrivia advances pos past any run of whitespace and comments, returning
// the byte offset of the next real token (or len(src) at end of file). This
// is how a "@target" annotation is matched to "the next construct": the
// construct's own Start must equal exactly this position, with nothing but
// trivia in between.
func skipTrivia(src []byte, pos int, comments []comment) int {
	for {
		for pos < len(src) && isSpace(src[pos]) {
			pos++
		}
		moved := false
		for _, c := range comments {
			if c.Start == pos {
				pos = c.End
				moved = true
				break
			}
		}
		if !moved {
			return pos
		}
	}
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

// collectCandidates walks the full acorn AST (decoded as a generic
// map[string]any/[]any tree) and returns every candidate construct, in
// source order, wherever it appears -- at top level, nested inside function
// bodies, or nested inside object literals.
func collectCandidates(node any) []candidate {
	var out []candidate
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			typ, _ := v["type"].(string)
			switch typ {
			case "Program", "BlockStatement":
				if body, ok := v["body"].([]any); ok {
					for _, stmt := range body {
						if sm, ok := stmt.(map[string]any); ok {
							if start, end, ok := nodeRange(sm); ok {
								out = append(out, candidate{start: start, end: end, name: statementName(sm)})
							}
						}
					}
				}
			case "ObjectExpression":
				if props, ok := v["properties"].([]any); ok {
					for _, p := range props {
						pm, ok := p.(map[string]any)
						if !ok {
							continue
						}
						if pt, _ := pm["type"].(string); pt != "Property" {
							continue // e.g. SpreadElement: not an annotatable construct
						}
						if start, end, ok := nodeRange(pm); ok {
							out = append(out, candidate{start: start, end: end, name: propertyName(pm)})
						}
					}
				}
			}
			for _, val := range v {
				walk(val)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(node)
	return out
}

func nodeRange(n map[string]any) (start, end int, ok bool) {
	s, sok := n["start"].(float64)
	e, eok := n["end"].(float64)
	if !sok || !eok {
		return 0, 0, false
	}
	return int(s), int(e), true
}

// statementName returns a best-effort diagnostic identifier for a
// Program/BlockStatement body element.
func statementName(n map[string]any) string {
	typ, _ := n["type"].(string)
	switch typ {
	case "FunctionDeclaration":
		if name, ok := identifierName(n["id"]); ok {
			return name
		}
	case "VariableDeclaration":
		decls, _ := n["declarations"].([]any)
		var names []string
		for _, d := range decls {
			dm, ok := d.(map[string]any)
			if !ok {
				continue
			}
			if name, ok := identifierName(dm["id"]); ok {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			return strings.Join(names, ", ")
		}
	}
	return typ
}

// propertyName returns a best-effort diagnostic identifier for an
// ObjectExpression Property.
func propertyName(n map[string]any) string {
	if computed, _ := n["computed"].(bool); computed {
		return "<computed property>"
	}
	key, ok := n["key"].(map[string]any)
	if !ok {
		return "<property>"
	}
	switch key["type"] {
	case "Identifier":
		if name, ok := identifierName(key); ok {
			return name
		}
	case "Literal":
		if v, ok := key["value"]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return "<property>"
}

func identifierName(n any) (string, bool) {
	m, ok := n.(map[string]any)
	if !ok {
		return "", false
	}
	name, ok := m["name"].(string)
	return name, ok
}

// checkNesting reports an error if any two Regions (sorted by Start) overlap
// -- in particular if one Region's Start falls strictly inside another's
// [Start, End) span, which is what a "@target daemon" construct nested
// inside another "@target daemon" (or any other target) construct produces.
//
// Checking only adjacent pairs is sufficient: since sortedRegions is sorted
// by Start, if a Region overlaps any later Region, it necessarily overlaps
// its immediate successor too (the successor's Start is the smallest of all
// the later Starts, so it is the first to fall inside the earlier Region's
// span, if any does).
func checkNesting(sortedRegions []Region) error {
	for i := 0; i+1 < len(sortedRegions); i++ {
		outer := sortedRegions[i]
		inner := sortedRegions[i+1]
		if inner.Start >= outer.End {
			continue
		}
		return fmt.Errorf(
			"jstarget: nested @target annotation: %q [%d,%d) target=%s contains %q [%d,%d) target=%s",
			outer.Name, outer.Start, outer.End, outer.Target,
			inner.Name, inner.Start, inner.End, inner.Target,
		)
	}
	return nil
}
