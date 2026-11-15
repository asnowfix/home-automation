package alert

import (
	"errors"
	"strings"
	"testing"
)

func valid() *Alert {
	return &Alert{V: 1, Fingerprint: "homedrive.offline/nas-fix", Rule: "homedrive.offline", Key: "nas-fix",
		Severity: Critical, State: Firing, Source: "homedrive", Since: 1763200000, Ts: 1763200300,
		Summary: "homedrive offline: fix@nas", Detail: "No status since 14:32 (16 min).",
		Labels: map[string]string{"subject": "homedrive:nas/fix"}}
}

func TestValidateAccepts(t *testing.T) {
	a := valid()
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	r := valid()
	r.State, r.Ended = Resolved, r.Since+60
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	// Labels are local only: they may hold a device ID.
	a.Labels = map[string]string{"subject": "shellytest1-02a1b2"}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejects(t *testing.T) {
	long := strings.Repeat("x", 101)
	tests := map[string]func(a *Alert){
		"version":        func(a *Alert) { a.V = 2 },
		"bad rule":       func(a *Alert) { a.Rule = "Home Drive" },
		"fingerprint":    func(a *Alert) { a.Fingerprint = "other/nas-fix" },
		"severity":       func(a *Alert) { a.Severity = "info" },
		"firing ended":   func(a *Alert) { a.Ended = 5 },
		"resolved ended": func(a *Alert) { a.State = Resolved },
		"summary length": func(a *Alert) { a.Summary = long },
		"detail length":  func(a *Alert) { a.Detail = strings.Repeat("x", 401) },
		"empty summary":  func(a *Alert) { a.Summary = "" },
		"too many labels": func(a *Alert) {
			a.Labels = map[string]string{}
			for _, k := range "abcdefghi" {
				a.Labels[string(k)] = "v"
			}
		},
		"label length": func(a *Alert) { a.Labels = map[string]string{"k": strings.Repeat("v", 65)} },
		"ipv4 summary": func(a *Alert) { a.Summary = "offline at 192.0.2.20" },
		"ipv4 detail":  func(a *Alert) { a.Detail = "last seen 192.0.2.7" },
		"mac colons":   func(a *Alert) { a.Detail = "mac 02:00:00:00:00:a1 silent" },
		"mac bare":     func(a *Alert) { a.Summary = "0200000000a1 offline" },
		"device id":    func(a *Alert) { a.Summary = "shellytest1-02a1b2 offline" },
		"unix path":    func(a *Alert) { a.Detail = "cannot read /var/lib/myhome/db" },
		"windows path": func(a *Alert) { a.Detail = `cannot read C:\Users\x` },
		"raw error":    func(a *Alert) { a.Detail = "dial tcp: connection refused" },
	}
	// Each case must be rejected for its own reason, not by accident.
	want := map[string]string{
		"version": "unsupported document version", "bad rule": "must match", "fingerprint": "fingerprint",
		"severity": "bad severity", "firing ended": "ended must be 0", "resolved ended": "ended must not precede",
		"summary length": "summary longer", "detail length": "detail longer", "empty summary": "empty summary",
		"too many labels": "more than 8 labels", "label length": "label longer",
		"ipv4 summary": "summary contains an IP", "ipv4 detail": "detail contains an IP",
		"mac colons": "detail contains a MAC", "mac bare": "summary contains a MAC",
		"device id": "summary contains a device ID", "unix path": "detail contains an absolute path",
		"windows path": "detail contains an absolute path", "raw error": "detail looks like raw error",
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			a := valid()
			mutate(a)
			err := a.Validate()
			if err == nil || !strings.Contains(err.Error(), want[name]) {
				t.Fatalf("Validate(%q) = %v, want an error containing %q", name, err, want[name])
			}
		})
	}
}

func TestEncodeDecode(t *testing.T) {
	b, err := Encode(valid())
	if err != nil {
		t.Fatal(err)
	}
	a, err := Decode(b)
	if err != nil || a.Fingerprint != "homedrive.offline/nas-fix" || a.Labels["subject"] == "" {
		t.Fatalf("round trip: %+v %v", a, err)
	}
	// Unknown fields are ignored; another version is a distinguishable error.
	if _, err := Decode([]byte(`{"v":1,"extra":1,"fingerprint":"r/k","rule":"r","key":"k","severity":"warning","state":"firing","source":"s","since":1,"ts":1,"summary":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode([]byte(`{"v":2}`)); !errors.Is(err, ErrVersion) {
		t.Fatalf("want ErrVersion, got %v", err)
	}
	bad := valid()
	bad.Summary = "192.0.2.3"
	if _, err := Encode(bad); err == nil {
		t.Fatal("Encode must validate")
	}
}

func TestTopicAndSupersedes(t *testing.T) {
	if got := Topic("myhome", "r.x", "k"); got != "myhome/alert/r.x/k" {
		t.Fatal(got)
	}
	old, newer := valid(), valid()
	newer.Since++
	if !newer.Supersedes(old) || old.Supersedes(newer) || old.Supersedes(old) {
		t.Fatal("supersede only by a strictly newer since")
	}
}
