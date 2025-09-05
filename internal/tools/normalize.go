package tools

import "strings"

// NormalizeMac takes a MAC address in various forms and returns a
// lowercased, colon-separated representation when possible.
// Examples:
//  - "E8E07EA60C6F"   -> "e8:e0:7e:a6:0c:6f"
//  - "e8-e0-7e-a6-0c-6f" -> "e8:e0:7e:a6:0c:6f"
//  - "e8:e0:7e:a6:0c:6f" -> unchanged
// If normalization is not possible, returns the trimmed, lower-cased input.
func NormalizeMac(in string) string {
	m := strings.ToLower(strings.TrimSpace(in))
	if m == "" {
		return ""
	}
	// If it already contains colons in canonical size, return as-is.
	if strings.Count(m, ":") == 5 && len(m) == 17 {
		return m
	}
	// Strip all non-hex characters
	onlyHex := make([]rune, 0, len(m))
	for _, r := range m {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			onlyHex = append(onlyHex, r)
		}
	}
	if len(onlyHex) != 12 {
		return m
	}
	var b strings.Builder
	for i, r := range onlyHex {
		if i > 0 && i%2 == 0 {
			b.WriteByte(':')
		}
		b.WriteRune(r)
	}
	return b.String()
}
