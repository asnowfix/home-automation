package roomba

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

const testBLID = "0123456789ABCDEF0123456789ABCDEF"

// --- password ---

func TestParsePassword(t *testing.T) {
	const pw = ":1:1700000000:AbCdEfGhIjKlMnOp" // 30 printable chars, fake
	// Shape of a real reply: binary header, the password, a trailing NUL.
	reply := append([]byte{0xf0, 0x23, 0xef, 0xcc, 0x3b, 0x29, 0x00, 0x05, 0x09, 0x00, 0x00, 0x00, 0x00}, []byte(pw)...)
	reply = append(reply, 0x00)

	got, err := ParsePassword(reply)
	if err != nil || got != pw {
		t.Fatalf("ParsePassword = %q, %v; want %q", got, err, pw)
	}

	// Regression for the dorita980 v4 bug: a fixed-offset slice keeps the header's
	// tail or the trailing NUL, which the robot then rejects at login.
	if fixed := string(reply[9:]); fixed == pw {
		t.Fatal("fixture no longer exercises the fixed-offset bug")
	}
}

func TestParsePasswordNeedsButton(t *testing.T) {
	_, err := ParsePassword([]byte{0xf0, 0x05, 0xef, 0xcc, 0x3b, 0x29, 0x00})
	if !errors.Is(err, ErrNeedsButton) {
		t.Fatalf("err = %v, want ErrNeedsButton", err)
	}
}

func TestParsePasswordNoRun(t *testing.T) {
	if _, err := ParsePassword([]byte("\x00\x01short\x00\x02also-short\x00")); err == nil {
		t.Fatal("expected an error when no printable run is long enough")
	}
}

// --- discovery ---

// fakeRobot answers discovery probes on a loopback UDP port.
func fakeRobot(t *testing.T, replies ...string) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 64)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if string(buf[:n]) != "irobotmcs" {
				continue
			}
			for _, r := range replies {
				_, _ = conn.WriteToUDP([]byte(r), from)
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr)
}

func TestDiscoverFind(t *testing.T) {
	addr := fakeRobot(t,
		`not json`,
		`{"hostname":"Printer-1","ip":"192.0.2.9"}`,
		`{"hostname":"Roomba-OTHER","ip":"192.0.2.7","robotname":"Other"}`,
		`{"hostname":"iRobot-`+testBLID+`","ip":"192.0.2.50","robotname":"Edmond","sku":"i715640","mac":"00:00:5e:00:53:01","sw":"lewis+22.52.10"}`,
	)
	d := &Discoverer{Addr: addr, Interval: 100 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	info, err := d.Find(ctx, testBLID)
	if err != nil {
		t.Fatal(err)
	}
	if info.IP != "192.0.2.50" || info.Name != "Edmond" || info.BLID != testBLID || info.SKU != "i715640" {
		t.Fatalf("unexpected info %+v", info)
	}
}

func TestDiscoverScanDedup(t *testing.T) {
	r := `{"hostname":"Roomba-AAA","ip":"192.0.2.1"}`
	addr := fakeRobot(t, r, r)
	d := &Discoverer{Addr: addr, Interval: 50 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	found, err := d.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].BLID != "AAA" {
		t.Fatalf("found = %+v", found)
	}
}

func TestDiscoverNotFound(t *testing.T) {
	addr := fakeRobot(t) // never answers
	d := &Discoverer{Addr: addr, Interval: 50 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := d.Find(ctx, testBLID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// --- TLS identity ---

func TestVerifyRobotCN(t *testing.T) {
	cs := func(cn string) tls.ConnectionState {
		return tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: cn}}}}
	}
	if err := verifyRobotCN(cs("iRobot-"+testBLID), testBLID); err != nil {
		t.Fatalf("matching CN rejected: %v", err)
	}
	if err := verifyRobotCN(cs("iRobot-SOMEONEELSE"), testBLID); err == nil {
		t.Fatal("foreign CN accepted")
	}
	if err := verifyRobotCN(tls.ConnectionState{}, testBLID); err == nil {
		t.Fatal("missing certificate accepted")
	}
	cfg := TLSConfig(testBLID)
	if cfg.MaxVersion != tls.VersionTLS12 || len(cfg.CipherSuites) != 1 || cfg.CipherSuites[0] != tls.TLS_RSA_WITH_AES_128_CBC_SHA256 {
		t.Fatalf("TLS config drifted from what the robot accepts: %+v", cfg)
	}
}

// --- state + commands ---

func push(c *Client, reported map[string]any) {
	b, _ := json.Marshal(map[string]any{"state": map[string]any{"reported": reported}})
	c.handle(b)
}

func TestStateMergeAndStatus(t *testing.T) {
	var seen []Status
	var mu sync.Mutex
	c := NewClient(logr.Discard(), testBLID, "pw", "192.0.2.50", func(s Status) {
		mu.Lock()
		seen = append(seen, s)
		mu.Unlock()
	})

	push(c, map[string]any{"name": "Edmond", "pmaps": []map[string]string{{"e8EOWqiwQ6e8sGpyYU4C-w": "260829T114918"}}})
	if _, ok := c.Status(); ok {
		t.Fatal("status reported ready before batPct and cleanMissionStatus arrived")
	}
	push(c, map[string]any{"batPct": 100, "bin": map[string]any{"full": false, "present": true}})
	push(c, map[string]any{"cleanMissionStatus": map[string]any{"phase": "charge", "cycle": "none", "error": 0, "mssnM": 0},
		"lastCommand": map[string]any{"command": "addrobotdone", "initiator": nil, "time": nil}})
	push(c, map[string]any{"batPct": 95}) // partial delta keeps the rest

	st, ok := c.Status()
	if !ok {
		t.Fatal("status not ready")
	}
	if st.Name != "Edmond" || st.Battery != 95 || !st.BinPresent || st.BinFull || st.Phase != PhaseCharge {
		t.Fatalf("unexpected status %+v", st)
	}
	if len(st.Maps) != 1 || st.Maps[0] != (Map{ID: "e8EOWqiwQ6e8sGpyYU4C-w", Version: "260829T114918"}) {
		t.Fatalf("maps = %+v", st.Maps)
	}
	if st.LastCommand.Command != "addrobotdone" || st.LastCommand.Time != 0 {
		t.Fatalf("lastCommand = %+v", st.LastCommand)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("onChange called %d times, want 2 (only once ready)", len(seen))
	}
}

func TestCommandAcknowledged(t *testing.T) {
	c := NewClient(logr.Discard(), testBLID, "pw", "192.0.2.50", nil)
	var sent map[string]any
	c.publish = func(topic string, payload []byte) error {
		if topic != "cmd" {
			t.Errorf("topic = %q", topic)
		}
		_ = json.Unmarshal(payload, &sent)
		// The robot echoes the command shortly after.
		go func() {
			time.Sleep(20 * time.Millisecond)
			push(c, map[string]any{"lastCommand": map[string]any{"command": sent["command"], "time": sent["time"], "initiator": "localApp"}})
		}()
		return nil
	}
	err := c.CleanRegions(context.Background(), Map{ID: "m1", Version: "v1"}, []Region{{ID: "11", Type: "rid"}})
	if err != nil {
		t.Fatal(err)
	}
	if sent["command"] != "start" || sent["pmap_id"] != "m1" || sent["user_pmapv_id"] != "v1" || sent["initiator"] != "localApp" {
		t.Fatalf("payload = %v", sent)
	}
	if _, ok := sent["password"]; ok {
		t.Fatal("password leaked into a command")
	}
}

func TestCommandNotAcknowledged(t *testing.T) {
	c := NewClient(logr.Discard(), testBLID, "pw", "192.0.2.50", nil)
	c.publish = func(string, []byte) error {
		// An echo of a different command must not count.
		go push(c, map[string]any{"lastCommand": map[string]any{"command": "dock", "time": time.Now().Unix()}})
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := c.Command(ctx, CmdStart, nil); err == nil {
		t.Fatal("unacknowledged command reported success")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.acks) != 0 {
		t.Fatalf("leaked %d ack waiters", len(c.acks))
	}
}
