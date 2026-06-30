package notify

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

func TestBuildMessage(t *testing.T) {
	msg := string(buildMessage("from@x.com", "to@x.com", "Hi", "Body text"))
	for _, want := range []string{"From: from@x.com", "To: to@x.com", "Subject: Hi", "Body text"} {
		if !strings.Contains(msg, want) {
			t.Errorf("buildMessage() missing %q in:\n%s", want, msg)
		}
	}
}

func TestSplitRecipients(t *testing.T) {
	got := splitRecipients(" a@x.com, b@x.com ,, c@x.com")
	want := []string{"a@x.com", "b@x.com", "c@x.com"}
	if len(got) != len(want) {
		t.Fatalf("splitRecipients() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitRecipients()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// startFakeSMTPServer runs a minimal SMTP server (no STARTTLS/AUTH
// extensions, so gmailMailer.Send skips both branches) that captures the
// DATA payload of a single session onto the returned channel.
func startFakeSMTPServer(t *testing.T) (addr string, dataCh chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	dataCh = make(chan string, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		writeLine := func(s string) {
			w.WriteString(s)
			w.WriteString("\r\n")
			w.Flush()
		}

		writeLine("220 fake.smtp ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				writeLine("250 fake.smtp") // no STARTTLS/AUTH advertised
			case strings.HasPrefix(upper, "MAIL FROM"):
				writeLine("250 OK")
			case strings.HasPrefix(upper, "RCPT TO"):
				writeLine("250 OK")
			case upper == "DATA":
				writeLine("354 Start mail input; end with <CRLF>.<CRLF>")
				var body strings.Builder
				for {
					dl, err := r.ReadString('\n')
					if err != nil {
						return
					}
					dl = strings.TrimRight(dl, "\r\n")
					if dl == "." {
						break
					}
					body.WriteString(dl)
					body.WriteString("\n")
				}
				dataCh <- body.String()
				writeLine("250 OK: queued")
			case upper == "QUIT":
				writeLine("221 Bye")
				return
			default:
				writeLine("500 unrecognized command")
			}
		}
	}()

	return ln.Addr().String(), dataCh
}

// TestGmailMailer_Send_RoundTrip exercises the full dial→EHLO→MAIL→RCPT→
// DATA→QUIT sequence against a fake local SMTP server and confirms the
// rendered message reaches the server intact.
func TestGmailMailer_Send_RoundTrip(t *testing.T) {
	addr, dataCh := startFakeSMTPServer(t)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("atoi port: %v", err)
	}

	m := &gmailMailer{
		log: logr.Discard(),
		cfg: Config{
			Host:    host,
			Port:    port,
			From:    "me@gmail.com",
			To:      "you@example.com",
			Timeout: 5 * time.Second,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Send(ctx, "Daily notice digest", "3 notices today."); err != nil {
		t.Fatalf("Send() = %v, want nil", err)
	}

	select {
	case body := <-dataCh:
		if !strings.Contains(body, "Subject: Daily notice digest") {
			t.Errorf("DATA missing subject header:\n%s", body)
		}
		if !strings.Contains(body, "3 notices today.") {
			t.Errorf("DATA missing body:\n%s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fake server never received DATA")
	}
}

// TestGmailMailer_Send_DialFailure confirms Send fails fast (does not hang
// the caller) when the SMTP server is unreachable — the digest scheduler
// relies on this to log-and-continue rather than block the daemon.
func TestGmailMailer_Send_DialFailure(t *testing.T) {
	m := &gmailMailer{
		log: logr.Discard(),
		cfg: Config{Host: "127.0.0.1", Port: 1, From: "me@gmail.com", To: "you@example.com", Timeout: 2 * time.Second},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.Send(ctx, "subject", "body"); err == nil {
		t.Fatal("Send() to unreachable port = nil error, want error")
	}
}
