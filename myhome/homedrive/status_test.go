package homedrive

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestParse_FixturesMatchContract strict-decodes the hand-written UI-edge-case
// fixtures (built from contract section 5.3). The producer's own documents are
// pinned by TestParse_ProducerDocumentsPinned.
func TestParse_FixturesMatchContract(t *testing.T) {
	for _, name := range []string{"running.json", "error.json", "auth_token_invalid.json"} {
		t.Run(name, func(t *testing.T) {
			b := fixture(t, name)
			dec := json.NewDecoder(bytes.NewReader(b))
			dec.DisallowUnknownFields()
			var strict Status
			if err := dec.Decode(&strict); err != nil {
				t.Fatalf("strict decode: %v", err)
			}
			st, err := Parse(b)
			if err != nil {
				t.Fatal(err)
			}
			if st.Unsupported || st.Schema != "2.0" || st.Ts.IsZero() || st.PublishInterval != 30 {
				t.Errorf("envelope = %+v", st)
			}
			for _, w := range Windows {
				if st.Counts[w] == nil {
					t.Errorf("counts[%s] missing", w)
				}
			}
		})
	}
}

// The files in testdata/contract are copied VERBATIM from the producer:
// github.com/asnowfix/home-drive, branch issue-141-status-v2, path
// homedrive/pkg/homedrive/testdata/status-{running,error,auth-failure,
// root-blocked,delete-blocked}.json (schema 2.1, PR #144 commit 42f88c2). Re-copy them when the contract changes; this test then fails if the
// copied structs lack a field the producer emits.
func TestParse_ProducerDocumentsPinned(t *testing.T) {
	tests := []struct {
		file  string
		check func(t *testing.T, st *Status)
	}{
		{"status-running.json", func(t *testing.T, st *Status) {
			c := st.Counts[Window7d]
			if st.State != StateRunning || c.ErrorsUp == nil || *c.ErrorsUp != 3 || c.ErrorsDown == nil || *c.ErrorsDown != 1 {
				t.Errorf("state=%q 7d=%+v", st.State, c)
			}
		}},
		{"status-error.json", func(t *testing.T, st *Status) {
			if st.State != StateError || st.LastError == nil {
				t.Errorf("state=%q last_error=%+v", st.State, st.LastError)
			}
		}},
		{"status-auth-failure.json", func(t *testing.T, st *Status) {
			if st.Auth == nil || st.Auth.State != "token_invalid" || *st.Counts[Window1h].ErrorsDown != 40 {
				t.Errorf("auth=%+v", st.Auth)
			}
		}},
		{"status-root-blocked.json", func(t *testing.T, st *Status) {
			g := st.Guards
			if st.State != StateRootBlocked || g == nil || g.BisyncDeletes != nil || g.LocalRoot == nil ||
				g.LocalRoot.Reason != RootReasonMarkerMissing || g.LocalRoot.Since.IsZero() {
				t.Errorf("state=%q guards=%+v", st.State, g)
			}
		}},
		{"status-delete-blocked.json", func(t *testing.T, st *Status) {
			g := st.Guards
			if st.State != StateRunning || g == nil || g.LocalRoot != nil || g.BisyncDeletes == nil {
				t.Fatalf("state=%q guards=%+v", st.State, g)
			}
			if d := g.BisyncDeletes; d.Reason != DeletesReasonThreshold || d.Planned != 412 || d.Threshold != 100 || d.Since.IsZero() {
				t.Errorf("bisync_deletes = %+v", d)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			b := fixture(t, "contract/"+tt.file)
			dec := json.NewDecoder(bytes.NewReader(b))
			dec.DisallowUnknownFields()
			var strict Status
			if err := dec.Decode(&strict); err != nil {
				t.Fatalf("strict decode: %v", err)
			}
			st, err := Parse(b)
			if err != nil || st.Unsupported {
				t.Fatalf("Parse: %v %+v", err, st)
			}
			if st.Schema != "2.1" || st.Guards == nil {
				t.Errorf("schema=%q guards=%v, want 2.1 with guards always present", st.Schema, st.Guards)
			}
			tt.check(t, st)
		})
	}
}

func TestParse_Running(t *testing.T) {
	st, err := Parse(fixture(t, "running.json"))
	if err != nil {
		t.Fatal(err)
	}
	c := st.Counts[Window24h]
	if c.FilesUp != 140 || c.BytesUp != 2100000000 || c.Errors != 2 || c.Starts != 1 {
		t.Errorf("24h = %+v", c)
	}
	if st.LastError == nil || st.LastError.Op != "push" || !strings.Contains(st.LastError.Message, "rateLimit") {
		t.Errorf("last_error = %+v", st.LastError)
	}
	if st.Auth.State != AuthOK || st.Bisync.Result != "ok" || st.Local.Files != 12345 || *st.QuotaUsedPct != 63.2 {
		t.Errorf("auth/bisync/local/quota = %+v %+v %+v %v", st.Auth, st.Bisync, st.Local, st.QuotaUsedPct)
	}
	if want := time.Date(2026, 11, 15, 14, 32, 0, 0, time.UTC); !st.Ts.Equal(want) {
		t.Errorf("ts = %v, want %v", st.Ts, want)
	}
}

func TestParse_NullFields(t *testing.T) {
	st, err := Parse(fixture(t, "error.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.LastPush != nil || st.LastPoll != nil || st.Bisync != nil || st.Local != nil || st.QuotaUsedPct != nil {
		t.Errorf("null fields should be nil: %+v", st)
	}
}

func TestParse_UnknownFieldsAndMinorBump(t *testing.T) {
	st, err := Parse(fixture(t, "schema_2_7_extra.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Unsupported {
		t.Fatal("2.7 must be accepted")
	}
	if st.State != "quota_blocked" || st.PublishInterval != 60 {
		t.Errorf("state/interval = %q/%d", st.State, st.PublishInterval)
	}
	c := st.Counts[Window1h]
	if c.ErrorsUp == nil || *c.ErrorsUp != 2 || *c.ErrorsDown != 0 {
		t.Errorf("errors_up/down = %v/%v", c.ErrorsUp, c.ErrorsDown)
	}
	if st.Counts[Window24h] != nil {
		t.Error("null window must be nil")
	}
	// the same document without the optional per-direction fields
	st, err = Parse(fixture(t, "running.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Counts[Window1h].ErrorsUp != nil {
		t.Error("absent errors_up must be nil")
	}
}

func TestParse_Unsupported(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantTs  bool
	}{
		{"major 3", string(fixture(t, "schema_3_0.json")), true},
		{"missing schema", `{"ts":"2026-11-15T14:32:00Z"}`, true},
		{"schema not a string", `{"schema":2,"ts":"2026-11-15T14:32:00Z"}`, true},
		{"schema garbage", `{"schema":"x.y"}`, false},
		{"major 1", `{"schema":"1.0","ts":"2026-11-15T14:32:00Z","state":7}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := Parse([]byte(tt.payload))
			if err != nil {
				t.Fatal(err)
			}
			if !st.Unsupported {
				t.Fatal("want Unsupported")
			}
			if st.Ts.IsZero() == tt.wantTs {
				t.Errorf("ts zero = %v, want %v", st.Ts.IsZero(), !tt.wantTs)
			}
			if st.State != "" || st.Counts != nil || st.LastError != nil {
				t.Errorf("contents must not be read from an unsupported document: %+v", st)
			}
		})
	}
}

func TestParse_Rejected(t *testing.T) {
	big := `{"schema":"2.0","ts":"2026-11-15T14:32:00Z","version":"` + strings.Repeat("x", MaxPayloadBytes) + `"}`
	tests := []struct {
		name    string
		payload string
		want    error
	}{
		{"oversize", big, ErrTooLarge},
		{"bad json", `{"schema":`, ErrBadJSON},
		{"not an object", `[1,2]`, ErrBadJSON},
		{"empty", ``, ErrBadJSON},
		{"v2 without ts", `{"schema":"2.0"}`, ErrBadJSON},
		{"v2 bad ts", `{"schema":"2.0","ts":"yesterday"}`, ErrBadJSON},
		{"v2 wrong type", `{"schema":"2.0","ts":"2026-11-15T14:32:00Z","counts":7}`, ErrBadJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.payload)); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestParse_PayloadAtLimit(t *testing.T) {
	pad := MaxPayloadBytes - len(`{"schema":"2.0","ts":"2026-11-15T14:32:00Z","version":""}`)
	p := `{"schema":"2.0","ts":"2026-11-15T14:32:00Z","version":"` + strings.Repeat("x", pad) + `"}`
	if len(p) != MaxPayloadBytes {
		t.Fatalf("len = %d", len(p))
	}
	if _, err := Parse([]byte(p)); err != nil {
		t.Errorf("a payload of exactly the limit must parse: %v", err)
	}
}

func TestStatus_IntervalOrDefault(t *testing.T) {
	var nilStatus *Status
	tests := []struct {
		st   *Status
		want time.Duration
	}{
		{nilStatus, 30 * time.Second},
		{&Status{}, 30 * time.Second},
		{&Status{PublishInterval: -5}, 30 * time.Second},
		{&Status{PublishInterval: 45}, 45 * time.Second},
	}
	for _, tt := range tests {
		if got := tt.st.IntervalOrDefault(); got != tt.want {
			t.Errorf("%+v: got %v, want %v", tt.st, got, tt.want)
		}
	}
}
