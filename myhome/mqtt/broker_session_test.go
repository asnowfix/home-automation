package mqtt

// Reproduction tests for #858: can the embedded Mochi broker leave the daemon's own subscription on
// myhome/rpc dead after the daemon's paho client reconnects?
//
// Everything runs in-process on 127.0.0.1: a Mochi server configured by loadBrokerConfig (the
// v0.14.1 cap of 16 pending writes), a "daemon" paho client with the same session options as
// NewClientE (CleanSession=false, AutoReconnect, ResumeSubs, OrderMatters=false) and a "ctl" paho
// client that publishes the requests. The daemon's connections go through gatedDialer, which lets
// a test hold a reconnect attempt at a chosen point, so each interleaving is forced, not hoped for.
// There is no fixed sleep: every wait is a bounded receive on a channel.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-logr/logr"
	mochiServer "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

const (
	rpcTopic        = "myhome/rpc"
	daemonClientID  = "myhome-myhome-test-1"
	ctlClientID     = "myhome-myhome-test-2"
	sessionWaitTime = 10 * time.Second
)

// startSessionBroker starts an in-process broker with the production configuration on a
// loopback port and returns it with its address.
func startSessionBroker(t *testing.T) (*mochiServer.Server, string) {
	t.Helper()
	opts := loadBrokerConfig(context.Background(), logr.Discard(), nil, 16) // v0.14.1 value
	opts.InlineClient = true
	opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := mochiServer.New(opts)
	if err := srv.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.AddListener(listeners.NewNet("t", ln)); err != nil {
		t.Fatal(err)
	}
	if err := srv.Serve(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, ln.Addr().String()
}

// deafConn wraps the daemon's TCP connection. deafen() makes paho's next Read fail and its Close a
// no-op, so paho believes the link is gone while the broker still sees an open socket: that is a
// half-open connection, which forces a session takeover on the next connect.
type deafConn struct {
	net.Conn
	deaf atomic.Bool
}

func (c *deafConn) deafen() {
	c.deaf.Store(true)
	_ = c.Conn.SetReadDeadline(time.Now()) // unblock paho's pending Read
}

func (c *deafConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if c.deaf.Load() {
		return 0, io.EOF
	}
	return n, err
}

func (c *deafConn) Close() error {
	if c.deaf.Load() {
		return nil // keep the broker's side open; closed by the test cleanup
	}
	return c.Conn.Close()
}

// gatedDialer opens the daemon's connections. While held, a connection attempt reports itself on
// attempts and then waits for release.
type gatedDialer struct {
	addr     string
	attempts chan struct{}

	mu      sync.Mutex
	held    bool
	release chan struct{}
	conns   []*deafConn
}

func newGatedDialer(t *testing.T, addr string) *gatedDialer {
	g := &gatedDialer{addr: addr, attempts: make(chan struct{}, 16)}
	t.Cleanup(func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.held {
			close(g.release)
			g.held = false
		}
		for _, c := range g.conns {
			_ = c.Conn.Close()
		}
	})
	return g
}

func (g *gatedDialer) hold() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.held, g.release = true, make(chan struct{})
}

func (g *gatedDialer) open() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held {
		close(g.release)
		g.held = false
	}
}

func (g *gatedDialer) last() *deafConn {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.conns[len(g.conns)-1]
}

func (g *gatedDialer) dial(_ *url.URL, _ mqtt.ClientOptions) (net.Conn, error) {
	g.mu.Lock()
	held, release := g.held, g.release
	g.mu.Unlock()
	if held {
		g.attempts <- struct{}{}
		<-release
	}
	c, err := net.Dial("tcp", g.addr)
	if err != nil {
		return nil, err
	}
	dc := &deafConn{Conn: c}
	g.mu.Lock()
	g.conns = append(g.conns, dc)
	g.mu.Unlock()
	return dc, nil
}

// daemonClient is a paho client with the session options of NewClientE, subscribed to myhome/rpc.
type daemonClient struct {
	mqtt.Client
	received  chan string
	connected chan struct{}
}

func startDaemonClient(t *testing.T, g *gatedDialer) *daemonClient {
	t.Helper()
	d := &daemonClient{received: make(chan string, 16), connected: make(chan struct{}, 16)}
	o := mqtt.NewClientOptions().
		AddBroker("tcp://" + g.addr).
		SetClientID(daemonClientID).
		SetAutoReconnect(true).
		SetResumeSubs(true).
		SetCleanSession(false).
		SetOrderMatters(false).
		SetKeepAlive(30 * time.Second).
		SetPingTimeout(10 * time.Second).
		SetConnectRetryInterval(5 * time.Second).
		SetCustomOpenConnectionFn(g.dial).
		SetOnConnectHandler(func(mqtt.Client) { d.connected <- struct{}{} })
	d.Client = mqtt.NewClient(o)
	waitToken(t, "daemon connect", d.Connect())
	<-d.connected
	waitToken(t, "daemon subscribe", d.Subscribe(rpcTopic, AtLeastOnce, func(_ mqtt.Client, m mqtt.Message) {
		d.received <- string(m.Payload())
	}))
	t.Cleanup(func() { d.Disconnect(0) })
	return d
}

func startCtlClient(t *testing.T, addr string) mqtt.Client {
	t.Helper()
	c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker("tcp://" + addr).SetClientID(ctlClientID))
	waitToken(t, "ctl connect", c.Connect())
	t.Cleanup(func() { c.Disconnect(0) })
	return c
}

func waitToken(t *testing.T, what string, tok mqtt.Token) {
	t.Helper()
	if !tok.WaitTimeout(sessionWaitTime) {
		t.Fatalf("%s: no completion within %s", what, sessionWaitTime)
	}
	if err := tok.Error(); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func waitSignal(t *testing.T, what string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(sessionWaitTime):
		t.Fatalf("%s: not observed within %s", what, sessionWaitTime)
	}
}

// requestDelivered publishes one request as ctl does (QoS 1 on myhome/rpc) and reports whether the
// daemon's handler received it within the bound.
func requestDelivered(t *testing.T, ctl mqtt.Client, d *daemonClient, id string) bool {
	t.Helper()
	payload := fmt.Sprintf(`{"id":%q,"method":"device.match","params":"*"}`, id)
	waitToken(t, "ctl publish "+id, ctl.Publish(rpcTopic, AtLeastOnce, false, payload))
	deadline := time.After(sessionWaitTime)
	for {
		select {
		case got := <-d.received:
			if got == payload {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// dropDaemonLink makes the broker close the daemon's connection while its reconnect is held, and
// returns once paho is waiting to reconnect and the broker sees the old client as closed.
func dropDaemonLink(t *testing.T, srv *mochiServer.Server, g *gatedDialer) *mochiServer.Client {
	t.Helper()
	old, ok := srv.Clients.Get(daemonClientID)
	if !ok {
		t.Fatal("daemon client not in the broker registry")
	}
	g.hold()
	old.Stop(errors.New("test: link dropped"))
	waitSignal(t, "daemon reconnect attempt", g.attempts)
	if !old.Closed() {
		t.Fatal("old daemon client should be closed")
	}
	return old
}

// The daemon's clean=false session is deleted by the 2-minute cleanup while it is disconnected;
// paho then reconnects without resubscribing (it ignores session-present). This is the #858
// hypothesis "the broker lost the daemon's session". Mochi keeps the subscription in its topic
// index under the client id, so delivery to the reconnected client must still work.
func TestBrokerSession_DeletedWhileDisconnected_RPCStillDelivered(t *testing.T) {
	srv, addr := startSessionBroker(t)
	g := newGatedDialer(t, addr)
	d := startDaemonClient(t, g)
	ctl := startCtlClient(t, addr)
	if !requestDelivered(t, ctl, d, "before") {
		t.Fatal("baseline request not delivered")
	}

	dropDaemonLink(t, srv, g)
	if n := removeClosedClients(logr.Discard(), srv.Clients, srv.Clients.GetAll()); n != 1 {
		t.Fatalf("cleanup removed %d clients, want 1 (the disconnected daemon)", n)
	}
	g.open()
	waitSignal(t, "daemon reconnected", d.connected)

	cl, ok := srv.Clients.Get(daemonClientID)
	if !ok {
		t.Fatal("reconnected daemon client not in the registry")
	}
	if n := cl.State.Subscriptions.Len(); n != 0 {
		t.Fatalf("reconnected client carries %d session subscriptions, want 0 (session was deleted)", n)
	}
	if _, indexed := srv.Topics.Subscribers(rpcTopic).Subscriptions[daemonClientID]; !indexed {
		t.Fatal("myhome/rpc no longer indexed for the daemon id")
	}
	if !requestDelivered(t, ctl, d, "after") {
		t.Fatal("request on myhome/rpc not delivered after the session was deleted and the client reconnected")
	}
}

// The daemon's paho client drops a half-open link and reconnects while the broker still holds the
// old connection: Mochi takes the session over. Delivery must survive the takeover.
func TestBrokerSession_TakeoverOfHalfOpenLink_RPCStillDelivered(t *testing.T) {
	srv, addr := startSessionBroker(t)
	g := newGatedDialer(t, addr)
	d := startDaemonClient(t, g)
	ctl := startCtlClient(t, addr)
	if !requestDelivered(t, ctl, d, "before") {
		t.Fatal("baseline request not delivered")
	}
	old, _ := srv.Clients.Get(daemonClientID)

	g.last().deafen()
	waitSignal(t, "daemon reconnected", d.connected)

	cl, ok := srv.Clients.Get(daemonClientID)
	if !ok || cl == old {
		t.Fatal("broker registry does not hold the new daemon connection")
	}
	if !old.Closed() {
		t.Fatal("old connection should have been closed by the takeover")
	}
	if !requestDelivered(t, ctl, d, "after") {
		t.Fatal("request on myhome/rpc not delivered after a session takeover")
	}
}
