package ui

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome/accounts"
)

func TestTruncateError(t *testing.T) {
	short := "beem: unauthorized"
	if got := truncateError(short); got != short {
		t.Errorf("truncateError(%q) = %q, want unchanged", short, got)
	}

	long := "beem: login failed with status 201: " + strings.Repeat("x", 200)
	got := truncateError(long)
	if len([]rune(got)) != maxErrorDisplayLen+1 { // +1 for the trailing ellipsis rune
		t.Errorf("truncateError(long) len = %d, want %d", len([]rune(got)), maxErrorDisplayLen+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncateError(long) = %q, want ellipsis suffix", got)
	}
}

// TestToAccountRows_TruncatesLongError verifies that a Beem-style login
// failure (which embeds the full JSON response body) is truncated for
// display while the full error is preserved for the hover tooltip.
func TestToAccountRows_TruncatesLongError(t *testing.T) {
	longErr := `beem: login failed with status 201: {"lastname":"DOE","firstname":"Jane","email":"jane.doe@example.com","userId":50969,"journeyStatus":"house_filled","countryCode":"FR","toggles":[],"isVerified":true,"accessToken":"tok-real-world","phoneNumber":"+33600000000","birthday":null,"civility":"sir","motivationForBeem":"energySelfSufficient"}`

	rows := toAccountRows([]accounts.Status{
		{Name: "beem", Enabled: true, LastOK: false, LastAttempt: time.Date(2026, 7, 4, 16, 56, 52, 0, time.UTC), LastError: longErr},
	})
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Name != "Beem Energy" {
		t.Errorf("Name = %q, want \"Beem Energy\"", row.Name)
	}
	if row.StatusClass != "is-danger" || row.StatusText != "Failed" {
		t.Errorf("status = (%q, %q), want (is-danger, Failed)", row.StatusClass, row.StatusText)
	}
	if len([]rune(row.LastError)) > maxErrorDisplayLen+1 {
		t.Errorf("LastError not truncated: len = %d", len([]rune(row.LastError)))
	}
	if row.LastErrorFull != longErr {
		t.Errorf("LastErrorFull = %q, want untruncated original", row.LastErrorFull)
	}

	var buf bytes.Buffer
	tmpl := template.Must(template.New("accounts-panel").Parse(accountsPanelTemplate))
	if err := tmpl.Execute(&buf, rows); err != nil {
		t.Fatalf("template.Execute: %v", err)
	}
	html := buf.String()

	// "energySelfSufficient" sits near the end of the JSON body, well past
	// maxErrorDisplayLen, so it must only appear inside the (escaped) title
	// tooltip attribute, never in the visible truncated text.
	const tailMarker = "energySelfSufficient"
	if strings.Contains(row.LastError, tailMarker) {
		t.Errorf("visible LastError not truncated: contains tail marker %q", tailMarker)
	}
	if !strings.Contains(html, tailMarker) {
		t.Errorf("rendered HTML missing full error (tail marker %q) in title tooltip attribute", tailMarker)
	}
	if !strings.Contains(html, `title="`) {
		t.Errorf("rendered HTML missing a title attribute for the error tooltip")
	}
	if !strings.Contains(html, "overflow-wrap: anywhere") {
		t.Errorf("rendered HTML missing overflow-wrap safety style on the error text")
	}
}
