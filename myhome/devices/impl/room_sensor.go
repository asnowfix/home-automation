package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// A room has at most one temperature sensor (#752, owner answer 1 on #684):
// assigning a second one is a configuration error. Never pick one silently,
// never average.

// roomHasSensorError builds the refusal for assigning d to a room that already
// holds other.
func roomHasSensorError(roomID string, other *myhome.Device) error {
	return fmt.Errorf("configuration error: room %q already has a temperature sensor, %s (%s); a room can have only one. Remove it first: myhome ctl room clear %s",
		roomID, other.Name(), other.Id(), other.Id())
}

// multipleSensorsMessage describes a room that already holds several sensors
// (data written before the rule), for room.setup.
func multipleSensorsMessage(roomID string, sensors []*myhome.Device) string {
	ids := make([]string, 0, len(sensors))
	for _, s := range sensors {
		ids = append(ids, s.Id())
	}
	return fmt.Sprintf("configuration error: room %q has %d temperature sensors (%s); a room can have only one. Keep one: myhome ctl room clear <id>",
		roomID, len(sensors), strings.Join(ids, ", "))
}

// checkRoomAssignment returns a configuration error when putting d in roomID
// would give that room a second temperature sensor. It returns nil when roomID
// is empty or when d is not a temperature sensor. Callers skip it when d is
// already in roomID.
func (dm *DeviceManager) checkRoomAssignment(ctx context.Context, d *myhome.Device, roomID string) error {
	if roomID == "" || !myhome.IsTemperatureSensor(d) {
		return nil
	}
	// Check-then-write is not atomic: two concurrent assignments could both pass. Accepted (single-user kiosk).
	all, err := dm.dr.GetAllDevices(ctx)
	if err != nil {
		return fmt.Errorf("check room %q for temperature sensors: %w", roomID, err)
	}
	return conflictingSensor(all, d, roomID)
}

// conflictingSensor returns the refusal if devices holds another temperature
// sensor (different id) in roomID. Pure, so the import pre-check can share it.
func conflictingSensor(devices []*myhome.Device, d *myhome.Device, roomID string) error {
	for _, o := range devices {
		if o.Id() != d.Id() && o.RoomId == roomID && myhome.IsTemperatureSensor(o) {
			return roomHasSensorError(roomID, o)
		}
	}
	return nil
}
