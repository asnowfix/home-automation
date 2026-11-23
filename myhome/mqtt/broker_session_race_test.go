package mqtt

import (
	"testing"

	"github.com/go-logr/logr"
)

// The 2-minute cleanup works from a snapshot of the registry. If the daemon reconnects between the
// snapshot and the deletion, the snapshot still holds the old, closed client under the same id, and
// a deletion by id removes the live successor. The successor's socket and keepalive keep working
// (Mochi answers PINGREQ on the connection itself), but every live publish is skipped because
// delivery resolves the id through the registry: the client is deaf on every topic until its next
// reconnect, and paho never sees a reason to reconnect.
func TestBrokerSession_CleanupSnapshotRace_KeepsLiveSuccessor(t *testing.T) {
	srv, addr := startSessionBroker(t)
	g := newGatedDialer(t, addr)
	d := startDaemonClient(t, g)
	ctl := startCtlClient(t, addr)
	if !requestDelivered(t, ctl, d, "before") {
		t.Fatal("baseline request not delivered")
	}

	dropDaemonLink(t, srv, g)
	snapshot := srv.Clients.GetAll() // the cleanup's GetAll(), taken while the daemon is away
	g.open()
	waitSignal(t, "daemon reconnected", d.connected)

	removeClosedClients(logr.Discard(), srv.Clients, snapshot)

	if _, ok := srv.Clients.Get(daemonClientID); !ok {
		t.Error("cleanup removed the live, reconnected daemon client from the registry")
	}
	if !d.IsConnected() {
		t.Fatal("daemon paho client should still be connected")
	}
	if !requestDelivered(t, ctl, d, "after") {
		t.Fatal("request on myhome/rpc not delivered after the cleanup ran on a stale snapshot")
	}
}
