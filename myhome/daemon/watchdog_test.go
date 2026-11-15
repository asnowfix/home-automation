package daemon

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mqttclient "github.com/asnowfix/home-automation/myhome/mqtt"
)

// recordingMailer stands in for SMTP: tests never send real email.
type recordingMailer struct{ subjects, bodies []string }

func (r *recordingMailer) Send(_ context.Context, subject, body string) error {
	r.subjects = append(r.subjects, subject)
	r.bodies = append(r.bodies, body)
	return nil
}

func fakeShow(props map[string]string, err error) showFunc {
	return func(context.Context, string) (map[string]string, error) { return props, err }
}

func TestHandleFailure(t *testing.T) {
	tests := []struct {
		name     string
		props    map[string]string
		showErr  error
		wantMail bool
	}{
		{"crash", map[string]string{"Result": "signal", "ExecMainStatus": "9", "RuntimeMaxUSec": "2d"}, nil, true},
		{"hang", map[string]string{"Result": "watchdog", "ExecMainStatus": "6", "RuntimeMaxUSec": "2d"}, nil, true},
		{"scheduled RuntimeMaxSec", map[string]string{"Result": "timeout", "RuntimeMaxUSec": "2d",
			"ActiveEnterTimestampMonotonic": "1000", "InactiveEnterTimestampMonotonic": "172800004000"}, nil, false},
		{"scheduled, short limit as measured", map[string]string{"Result": "timeout", "RuntimeMaxUSec": "4s",
			"ActiveEnterTimestampMonotonic": "801148033411", "InactiveEnterTimestampMonotonic": "801152245826"}, nil, false},
		{"timeout well before the limit (start/stop timeout)", map[string]string{"Result": "timeout", "RuntimeMaxUSec": "2d",
			"ActiveEnterTimestampMonotonic": "1000", "InactiveEnterTimestampMonotonic": "3000000"}, nil, true},
		{"timeout with no RuntimeMaxSec", map[string]string{"Result": "timeout", "RuntimeMaxUSec": "infinity",
			"ActiveEnterTimestampMonotonic": "1000", "InactiveEnterTimestampMonotonic": "999999999999"}, nil, true},
		{"systemctl unavailable alerts", nil, errors.New("exec: not found"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &recordingMailer{}
			var out bytes.Buffer
			if err := handleFailure(context.Background(), "myhome.service", fakeShow(tt.props, tt.showErr), m, &out); err != nil {
				t.Fatal(err)
			}
			if got := len(m.subjects) == 1; got != tt.wantMail {
				t.Fatalf("emails sent = %d, want mail=%v", len(m.subjects), tt.wantMail)
			}
			if len(m.subjects) > 1 {
				t.Fatalf("more than one email: %v", m.subjects)
			}
		})
	}
}

func TestParseTimespan(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"4s": 4 * time.Second, "2d": 48 * time.Hour, "1min 30s": 90 * time.Second, "500ms": 500 * time.Millisecond,
	} {
		if got, ok := parseTimespan(in); !ok || got != want {
			t.Errorf("parseTimespan(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "infinity", "abc", "5parsecs"} {
		if _, ok := parseTimespan(in); ok {
			t.Errorf("parseTimespan(%q) should fail", in)
		}
	}
}

// echoClient is an MQTT client whose broker echoes every publish back to the
// subscriber, or drops it when broken.
type echoClient struct {
	mqttclient.Client
	ch     chan []byte
	broken bool
}

func (e *echoClient) Subscribe(context.Context, string, uint, string) (<-chan []byte, error) {
	return e.ch, nil
}

func (e *echoClient) Publish(_ context.Context, _ string, msg []byte, _ byte, _ bool, _ string) error {
	if !e.broken {
		e.ch <- msg
	}
	return nil
}

func TestMQTTRoundTripCheck(t *testing.T) {
	c := &echoClient{ch: make(chan []byte, 8)}
	check, err := mqttRoundTripCheck(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if err := check(context.Background()); err != nil {
		t.Fatalf("healthy broker: %v", err)
	}
	c.broken = true
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = check(ctx)
	if err == nil || !strings.Contains(err.Error(), "no echo") {
		t.Fatalf("wedged broker: got %v, want no-echo error", err)
	}
}
