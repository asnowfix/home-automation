package ui

import (
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// The Rooms list card follows the robot without a reload (#822). Like the other
// clean tests, these must not call t.Parallel(): the method registry is global.

func cardEndpoint(t *testing.T, room string) (int, string) {
	t.Helper()
	return roomBody(t, nil, "/htmx/room/"+room+"/clean-card")
}

func TestRoomCleanCardEndpointFollowsTheRobot(t *testing.T) {
	e := newCardEnv(t, idleRobot("R1"))
	e.regions["R1"] = []myhome.VacuumRegion{reg("m1", "1", "Salon", "sejour")}

	// robot changed its DHCP lease: disconnected, old host
	off := idleRobot("R1")
	off.Connected = false
	off.Host = "192.168.1.50"
	e.robots = []myhome.VacuumRobot{off}
	code, body := cardEndpoint(t, "sejour")
	if code != 200 || !strings.Contains(body, "data-room-clean-card") {
		t.Fatalf("offline: code=%d body=%s", code, body)
	}
	if btn := buttonTag(t, body, "Clean"); !strings.Contains(btn, "disabled") || !strings.Contains(body, "Robot offline (last seen at 192.168.1.50)") {
		t.Errorf("offline robot: want disabled Clean with the offline reason:\n%s", body)
	}
	if strings.Contains(body, "data-room-clean-confirm") {
		t.Errorf("offline robot: no confirm expected:\n%s", body)
	}

	// reconnected at its new address and docked: enabled, no reason
	e.robots = []myhome.VacuumRobot{idleRobot("R1")}
	code, body = cardEndpoint(t, "sejour")
	if code != 200 || strings.Contains(body, "data-room-clean-reason") || !strings.Contains(body, "data-room-clean-confirm") {
		t.Errorf("reconnected robot: want enabled Clean:\n%s", body)
	}
	if btn := buttonTag(t, body, "Clean"); strings.Contains(btn, "disabled") {
		t.Errorf("reconnected robot: Clean still disabled: %s", btn)
	}
	// rendering never starts a clean (the only device-facing verb is vacuum.clean)
	if len(e.cleaned) != 0 {
		t.Errorf("render must not start a clean: %v", e.cleaned)
	}
}

func TestRoomCleanCardEndpointEdges(t *testing.T) {
	e := newCardEnv(t, idleRobot("R1"))
	e.regions["R1"] = []myhome.VacuumRegion{reg("m1", "1", "Salon", "sejour")}
	if code, _ := cardEndpoint(t, "nope"); code != 404 {
		t.Errorf("unknown room = %d, want 404 (htmx leaves the card alone)", code)
	}
	// a room without a mapped area: empty fragment
	if code, body := cardEndpoint(t, "cuisine"); code != 200 || strings.TrimSpace(body) != "" {
		t.Errorf("cuisine: code=%d body=%q, want an empty fragment", code, body)
	}
}

// Every card polls its own endpoint and skips the tick while its confirm is open.
func TestRoomCardPollsAndGatesOnConfirm(t *testing.T) {
	e := newCardEnv(t, idleRobot("R1"))
	e.regions["R1"] = []myhome.VacuumRegion{reg("m1", "1", "Salon", "sejour")}
	card := cardOf(t, e.roomsList(t), "Séjour")
	head := card[:strings.Index(card, "x-data")]
	for _, want := range []string{`hx-get="/htmx/room/sejour/clean-card"`, `hx-swap="outerHTML"`, "every 5s",
		"kiosk.online", "kiosk.visible('rooms')", "!this.closest('[data-confirming]')"} {
		if !strings.Contains(head, want) {
			t.Errorf("card missing %q in:\n%s", want, head)
		}
	}
}
