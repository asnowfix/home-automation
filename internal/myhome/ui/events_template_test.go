package ui

import (
	"html/template"
	"strings"
	"testing"
)

func severityClassFunc(t *testing.T) func(string) string {
	t.Helper()
	fn, ok := eventTemplateFuncs()["severityClass"].(func(string) string)
	if !ok {
		t.Fatal("eventTemplateFuncs()[\"severityClass\"] is not a func(string) string")
	}
	return fn
}

func TestSeverityClass(t *testing.T) {
	severityClass := severityClassFunc(t)

	cases := []struct {
		severity string
		want     string
	}{
		{"alarm", "has-text-danger"},
		{"warn", "has-text-warning"},
		{"notice", "has-text-link"},
		{"debug", "has-text-grey-light"},
		{"info", ""},
		{"", ""},
		{"unknown", ""},
	}
	for _, c := range cases {
		if got := severityClass(c.severity); got != c.want {
			t.Errorf("severityClass(%q) = %q, want %q", c.severity, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	fn, ok := eventTemplateFuncs()["truncate"].(func(*string) string)
	if !ok {
		t.Fatal("eventTemplateFuncs()[\"truncate\"] is not a func(*string) string")
	}
	if got := fn(nil); got != "" {
		t.Errorf("truncate(nil) = %q, want empty", got)
	}
	short := "short value"
	if got := fn(&short); got != short {
		t.Errorf("truncate(short) = %q, want unchanged %q", got, short)
	}
	long := ""
	for range 100 {
		long += "x"
	}
	got := fn(&long)
	wantPrefix := long[:60]
	if !strings.HasPrefix(got, wantPrefix) || !strings.HasSuffix(got, "…") || got != wantPrefix+"…" {
		t.Errorf("truncate(long) = %q, want %q", got, wantPrefix+"…")
	}
}

func TestEventDataCell(t *testing.T) {
	fn, ok := eventTemplateFuncs()["eventDataCell"].(func(*string) template.HTML)
	if !ok {
		t.Fatal("eventTemplateFuncs()[\"eventDataCell\"] is not a func(*string) template.HTML")
	}
	if got := fn(nil); got != "" {
		t.Errorf("eventDataCell(nil) = %q, want empty", got)
	}
	empty := ""
	if got := fn(&empty); got != "" {
		t.Errorf("eventDataCell(empty) = %q, want empty", got)
	}
	validJSON := `{"mode":"summer"}`
	if got := fn(&validJSON); !strings.Contains(string(got), "<details") {
		t.Errorf("eventDataCell(validJSON) = %q, want a <details> element", got)
	}
	notJSON := "not json at all"
	if got := fn(&notJSON); !strings.Contains(string(got), "<span>") {
		t.Errorf("eventDataCell(notJSON) = %q, want a plain <span>", got)
	}
}

