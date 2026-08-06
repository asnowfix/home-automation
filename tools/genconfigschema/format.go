package main

import "go/format"

// formatGo gofmt's generated Go source so the checked-in (gitignored) output
// matches normal formatting instead of the generator's raw indentation.
func formatGo(src string) (string, error) {
	out, err := format.Source([]byte(src))
	if err != nil {
		return "", err
	}
	return string(out), nil
}
