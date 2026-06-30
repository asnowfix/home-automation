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

// fakeSMTPOpts configures the protocol-level behavior of startFakeSMTPServer,
// letting individual tests exercise gmailMailer.Send's error-handling
// branches (rejected MAIL/RCPT/DATA/AUTH, an unparseable greeting, an
// advertised-but-unusable STARTTLS) without a real SMTP server or a trusted
// TLS certificate.
type fakeSMTPOpts struct {
	greeting       string // default "220 fake.smtp ESMTP"
	advertiseTLS   bool   // advertise STARTTLS in EHLO; the upgrade itself is never completed
	advertiseAuth  bool   // advertise AUTH PLAIN in EHLO
	rejectAuth     bool   // reply 535 to AUTH instead of 235
	rejectMailFrom bool   // reply 550 to MAIL FROM
	rejectRcpt     bool   // reply 550 to RCPT TO
	rejectData     bool   // reply 550 to DATA

	// dropAfterDataPrompt closes the connection right after the "354 ..."
	// prompt, before reading the message body — simulating the connection
	// breaking mid-transfer so the client's write/close on the DATA stream
	// fails instead of completing normally.
	dropAfterDataPrompt bool
}

// startFakeSMTPServer runs a minimal, single-session SMTP server driven by
// opts, capturing the DATA payload (if reached) onto the returned channel.
func startFakeSMTPServer(t *testing.T, opts fakeSMTPOpts) (addr string, dataCh chan string) {
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

		greeting := opts.greeting
		if greeting == "" {
			greeting = "220 fake.smtp ESMTP"
		}
		writeLine(greeting)

		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				var caps []string
				if opts.advertiseTLS {
					caps = append(caps, "STARTTLS")
				}
				if opts.advertiseAuth {
					caps = append(caps, "AUTH PLAIN")
				}
				if len(caps) == 0 {
					writeLine("250 fake.smtp")
					continue
				}
				writeLine("250-fake.smtp")
				for i, c := range caps {
					if i == len(caps)-1 {
						writeLine("250 " + c)
					} else {
						writeLine("250-" + c)
					}
				}
			case upper == "STARTTLS":
				// Acknowledge the upgrade request but never actually speak
				// TLS: the client's following ClientHello bytes hit a plain
				// connection that we then close, so its handshake fails —
				// exercising client.StartTLS's error path without needing a
				// certificate the client would trust.
				writeLine("220 Go ahead")
				return
			case strings.HasPrefix(upper, "AUTH"):
				if opts.rejectAuth {
					writeLine("535 5.7.8 Authentication failed")
				} else {
					writeLine("235 2.7.0 Authentication successful")
				}
			case strings.HasPrefix(upper, "MAIL FROM"):
				if opts.rejectMailFrom {
					writeLine("550 Mail from rejected")
				} else {
					writeLine("250 OK")
				}
			case strings.HasPrefix(upper, "RCPT TO"):
				if opts.rejectRcpt {
					writeLine("550 Recipient rejected")
				} else {
					writeLine("250 OK")
				}
			case upper == "DATA":
				if opts.rejectData {
					writeLine("550 Data rejected")
					continue
				}
				writeLine("354 Start mail input; end with <CRLF>.<CRLF>")
				if opts.dropAfterDataPrompt {
					return // defer conn.Close() fires, breaking the client's write/close
				}
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

// newTestMailer builds a gmailMailer pointed at addr (host:port from
// startFakeSMTPServer) with the given extra config overrides.
func newTestMailer(t *testing.T, addr string, cfg Config) *gmailMailer {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("atoi port: %v", err)
	}
	cfg.Host = host
	cfg.Port = port
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &gmailMailer{log: logr.Discard(), cfg: cfg}
}

// TestGmailMailer_Send_RoundTrip exercises the full dial→EHLO→MAIL→RCPT→
// DATA→QUIT sequence against a fake local SMTP server and confirms the
// rendered message reaches the server intact. Also exercises the
// no-deadline-in-context branch (ctx.Deadline() not set, cfg.Timeout used
// instead), since callers like the notice digest scheduler pass a bare ctx.
func TestGmailMailer_Send_RoundTrip(t *testing.T) {
	addr, dataCh := startFakeSMTPServer(t, fakeSMTPOpts{})
	m := newTestMailer(t, addr, Config{From: "me@gmail.com", To: "you@example.com"})

	if err := m.Send(context.Background(), "Daily notice digest", "3 notices today."); err != nil {
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

// TestGmailMailer_Send_AuthSucceeds exercises the AUTH-advertised,
// auth-succeeds path end to end (Username set, server accepts AUTH PLAIN).
func TestGmailMailer_Send_AuthSucceeds(t *testing.T) {
	addr, dataCh := startFakeSMTPServer(t, fakeSMTPOpts{advertiseAuth: true})
	m := newTestMailer(t, addr, Config{From: "me@gmail.com", To: "you@example.com", Username: "me@gmail.com", Password: "app-password"})

	if err := m.Send(context.Background(), "s", "b"); err != nil {
		t.Fatalf("Send() = %v, want nil", err)
	}
	select {
	case <-dataCh:
	case <-time.After(2 * time.Second):
		t.Fatal("fake server never received DATA")
	}
}

// TestGmailMailer_Send_AuthFails confirms an AUTH rejection is wrapped and
// surfaced rather than silently swallowed.
func TestGmailMailer_Send_AuthFails(t *testing.T) {
	addr, _ := startFakeSMTPServer(t, fakeSMTPOpts{advertiseAuth: true, rejectAuth: true})
	m := newTestMailer(t, addr, Config{From: "me@gmail.com", To: "you@example.com", Username: "me@gmail.com", Password: "wrong"})

	err := m.Send(context.Background(), "s", "b")
	if err == nil || !strings.Contains(err.Error(), "smtp auth:") {
		t.Fatalf("Send() = %v, want error wrapped with \"smtp auth:\"", err)
	}
}

// TestGmailMailer_Send_STARTTLSFailure confirms a server advertising
// STARTTLS but failing the upgrade produces a wrapped "smtp starttls:"
// error rather than silently falling back to plaintext.
func TestGmailMailer_Send_STARTTLSFailure(t *testing.T) {
	addr, _ := startFakeSMTPServer(t, fakeSMTPOpts{advertiseTLS: true})
	m := newTestMailer(t, addr, Config{From: "me@gmail.com", To: "you@example.com"})

	err := m.Send(context.Background(), "s", "b")
	if err == nil || !strings.Contains(err.Error(), "smtp starttls:") {
		t.Fatalf("Send() = %v, want error wrapped with \"smtp starttls:\"", err)
	}
}

// TestGmailMailer_Send_BadGreeting confirms a malformed server greeting
// (smtp.NewClient failure) is wrapped as "smtp client:".
func TestGmailMailer_Send_BadGreeting(t *testing.T) {
	addr, _ := startFakeSMTPServer(t, fakeSMTPOpts{greeting: "500 borked"})
	m := newTestMailer(t, addr, Config{From: "me@gmail.com", To: "you@example.com"})

	err := m.Send(context.Background(), "s", "b")
	if err == nil || !strings.Contains(err.Error(), "smtp client:") {
		t.Fatalf("Send() = %v, want error wrapped with \"smtp client:\"", err)
	}
}

// TestGmailMailer_Send_NoRecipients confirms an empty To produces a clear
// error instead of silently calling DATA with zero RCPT TO targets.
func TestGmailMailer_Send_NoRecipients(t *testing.T) {
	addr, _ := startFakeSMTPServer(t, fakeSMTPOpts{})
	m := newTestMailer(t, addr, Config{From: "me@gmail.com", To: ""})

	err := m.Send(context.Background(), "s", "b")
	if err == nil || !strings.Contains(err.Error(), "no recipients") {
		t.Fatalf("Send() = %v, want a \"no recipients\" error", err)
	}
}

// TestGmailMailer_Send_MailFromRejected confirms a MAIL FROM rejection is
// wrapped as "smtp mail from:".
func TestGmailMailer_Send_MailFromRejected(t *testing.T) {
	addr, _ := startFakeSMTPServer(t, fakeSMTPOpts{rejectMailFrom: true})
	m := newTestMailer(t, addr, Config{From: "me@gmail.com", To: "you@example.com"})

	err := m.Send(context.Background(), "s", "b")
	if err == nil || !strings.Contains(err.Error(), "smtp mail from:") {
		t.Fatalf("Send() = %v, want error wrapped with \"smtp mail from:\"", err)
	}
}

// TestGmailMailer_Send_RcptRejected confirms a RCPT TO rejection is wrapped
// as "smtp rcpt".
func TestGmailMailer_Send_RcptRejected(t *testing.T) {
	addr, _ := startFakeSMTPServer(t, fakeSMTPOpts{rejectRcpt: true})
	m := newTestMailer(t, addr, Config{From: "me@gmail.com", To: "you@example.com"})

	err := m.Send(context.Background(), "s", "b")
	if err == nil || !strings.Contains(err.Error(), "smtp rcpt") {
		t.Fatalf("Send() = %v, want error wrapped with \"smtp rcpt\"", err)
	}
}

// TestGmailMailer_Send_DataRejected confirms a DATA rejection is wrapped as
// "smtp data:".
func TestGmailMailer_Send_DataRejected(t *testing.T) {
	addr, _ := startFakeSMTPServer(t, fakeSMTPOpts{rejectData: true})
	m := newTestMailer(t, addr, Config{From: "me@gmail.com", To: "you@example.com"})

	err := m.Send(context.Background(), "s", "b")
	if err == nil || !strings.Contains(err.Error(), "smtp data:") {
		t.Fatalf("Send() = %v, want error wrapped with \"smtp data:\"", err)
	}
}

// TestGmailMailer_Send_ConnectionDroppedDuringData confirms a connection
// that breaks mid-transfer surfaces as a wrapped "smtp write:" or
// "smtp close data:" error rather than hanging or panicking.
func TestGmailMailer_Send_ConnectionDroppedDuringData(t *testing.T) {
	addr, _ := startFakeSMTPServer(t, fakeSMTPOpts{dropAfterDataPrompt: true})
	m := newTestMailer(t, addr, Config{From: "me@gmail.com", To: "you@example.com"})

	err := m.Send(context.Background(), "s", "b")
	if err == nil {
		t.Fatal("Send() over a dropped connection = nil error, want error")
	}
	if !strings.Contains(err.Error(), "smtp write:") && !strings.Contains(err.Error(), "smtp close data:") {
		t.Fatalf("Send() = %v, want error wrapped with \"smtp write:\" or \"smtp close data:\"", err)
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
