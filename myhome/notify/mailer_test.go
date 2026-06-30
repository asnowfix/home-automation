package notify

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
)

func TestNew_NoopWhenFromEmpty(t *testing.T) {
	m := New(logr.Discard(), Config{Host: "smtp.gmail.com", Port: 587})
	if _, ok := m.(*noopMailer); !ok {
		t.Fatalf("New() with empty From = %T, want *noopMailer", m)
	}
}

func TestNew_GmailWhenFromSet(t *testing.T) {
	m := New(logr.Discard(), Config{Host: "smtp.gmail.com", Port: 587, From: "me@gmail.com", To: "you@example.com"})
	if _, ok := m.(*gmailMailer); !ok {
		t.Fatalf("New() with From set = %T, want *gmailMailer", m)
	}
}

// TestNoopMailer_Send verifies the no-op path never touches the network and
// never errors — disabled email must be silent, not a daemon failure mode.
func TestNoopMailer_Send(t *testing.T) {
	m := New(logr.Discard(), Config{})
	if err := m.Send(context.Background(), "subject", "body"); err != nil {
		t.Fatalf("noopMailer.Send() = %v, want nil", err)
	}
}
