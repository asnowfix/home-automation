package myhome

import "time"

// Robot vacuum RPC types (#606). Robots are addressed by id (the robot's BLID,
// which is public on the LAN) or by name; an empty Robot means "the only robot"
// and fails when there are several. The robot's local password never appears in
// any of these types.

// VacuumParams selects one robot.
type VacuumParams struct {
	Robot string `json:"robot,omitempty"`
}

// VacuumStatus is a robot's live state, refreshed by the robot's own pushes.
type VacuumStatus struct {
	Battery        int         `json:"battery"`
	BinFull        bool        `json:"bin_full"`
	BinPresent     bool        `json:"bin_present"`
	Phase          string      `json:"phase"` // charge, run, stop, hmUsrDock, hmMidMsn, hmPostMsn, stuck, evac
	Cycle          string      `json:"cycle"` // none, clean, dock, ...
	Error          int         `json:"error"`
	NotReady       int         `json:"not_ready"`
	MissionMinutes int         `json:"mission_minutes"`
	LastCommand    string      `json:"last_command,omitempty"`
	Maps           []VacuumMap `json:"maps,omitempty"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

// VacuumMap is one of the robot's persistent maps and its current version token.
type VacuumMap struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// VacuumRobot describes one paired robot.
type VacuumRobot struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Host      string        `json:"host,omitempty"`
	Connected bool          `json:"connected"`
	Status    *VacuumStatus `json:"status,omitempty"` // nil until the robot has reported once
}

// VacuumListResult is the result of vacuum.list.
type VacuumListResult struct {
	Robots []VacuumRobot `json:"robots"`
}

// VacuumCommandParams is a plain command: start (whole home), stop, pause, resume, dock, find.
type VacuumCommandParams struct {
	Robot   string `json:"robot,omitempty"`
	Command string `json:"command"`
}

// VacuumCleanParams cleans the given rooms, in order. Rooms are region names or region ids.
type VacuumCleanParams struct {
	Robot string   `json:"robot,omitempty"`
	Rooms []string `json:"rooms"`
}

// VacuumRegion is a room or zone the robot knows on one of its maps. Regions are
// learned from the robot's lastCommand whenever a room clean is started, from
// here or from the iRobot app; the name is ours to give.
type VacuumRegion struct {
	MapID      string `json:"map_id"`
	MapVersion string `json:"map_version"` // the map version the region was learned on
	ID         string `json:"id"`
	Type       string `json:"type"` // rid (room) or zid (zone)
	Name       string `json:"name,omitempty"`
	Stale      bool   `json:"stale"` // the map has changed since; the id may no longer be valid
}

// VacuumRoomsResult is the result of vacuum.rooms.
type VacuumRoomsResult struct {
	Regions []VacuumRegion `json:"regions"`
}

// VacuumRoomNameParams names a learned region.
type VacuumRoomNameParams struct {
	Robot    string `json:"robot,omitempty"`
	RegionID string `json:"region_id"`
	Name     string `json:"name"`
}

// VacuumPairParams pairs a robot whose HOME button was just held ~2 s.
// Host is optional: without it the robot is found by discovery, and must be the
// only unpaired robot answering.
type VacuumPairParams struct {
	Host string `json:"host,omitempty"`
}

// VacuumOKResult acknowledges a command the robot confirmed.
type VacuumOKResult struct {
	OK bool `json:"ok"`
}

// VacuumPhaseText describes a robot phase in words, for the CLI and the web UI.
func VacuumPhaseText(phase string) string {
	switch phase {
	case "charge":
		return "docked, charging"
	case "run":
		return "cleaning"
	case "stop":
		return "stopped"
	case "hmUsrDock":
		return "returning to dock"
	case "hmMidMsn":
		return "returning to recharge"
	case "hmPostMsn":
		return "returning after cleaning"
	case "stuck":
		return "stuck, needs help"
	case "evac":
		return "emptying bin"
	case "":
		return "unknown"
	}
	return phase
}
