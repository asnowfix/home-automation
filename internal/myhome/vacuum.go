package myhome

import "time"

// Robot vacuum RPC types (#606). Robots are addressed by id (the robot's BLID,
// which is public on the LAN) or by name; an empty Robot means "the only robot"
// and fails when there are several. The robot's local password never appears in
// any of these types: pairing runs in the CLI (`myhome ctl vacuum pair`), not over
// RPC, and the daemon reads the password from MYHOME_VACUUM_PASSWORD (#626).

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

// Where a region came from (VacuumRegion.Source).
const (
	VacuumSourceRobot = "robot" // learned from the robot's lastCommand after a room clean
	VacuumSourceCloud = "cloud" // fetched from the iRobot cloud account (vacuum.roomsync)
)

// VacuumRegion is a room or zone on one of the robot's maps. Regions come from
// the iRobot cloud account (vacuum.roomsync, #628), which is the source of truth
// for their names, or are learned from the robot's lastCommand whenever a room
// clean is started, from here or from the iRobot app.
type VacuumRegion struct {
	MapID      string `json:"map_id"`
	MapName    string `json:"map_name,omitempty"` // from the cloud
	MapVersion string `json:"map_version"`        // the map version the region was learned or fetched on
	ID         string `json:"id"`
	Type       string `json:"type"`           // rid (room) or zid (zone)
	Kind       string `json:"kind,omitempty"` // the cloud's room type: kitchen, bedroom, ...
	Name       string `json:"name,omitempty"`
	Source     string `json:"source"` // VacuumSourceRobot or VacuumSourceCloud
	// RobotMapVersion is the version the robot reports for this map right now
	// (empty until the robot has reported). For a cloud region it normally
	// differs from MapVersion: the robot's version moves on after every clean,
	// the cloud's stays at the last map confirmed in the app.
	RobotMapVersion string `json:"robot_map_version,omitempty"`
	// Unverified is set on a cloud region whose map version differs from the
	// robot's: the id is believed valid but has not been confirmed on this map.
	Unverified bool `json:"unverified,omitempty"`
	// Stale: for a learned region, the robot's map changed since it was learned;
	// for a cloud region, the last sync no longer listed it. Stale regions are
	// not cleaned.
	Stale bool `json:"stale"`
}

// VacuumRoomsResult is the result of vacuum.rooms.
type VacuumRoomsResult struct {
	Regions []VacuumRegion `json:"regions"`
	// CloudNames is true when the iRobot account is configured: room names
	// then come from the cloud and vacuum.roomname refuses cloud regions.
	CloudNames bool `json:"cloud_names"`
}

// VacuumSyncedMap summarises one map fetched by vacuum.roomsync.
type VacuumSyncedMap struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CloudVersion string `json:"cloud_version"`
	RobotVersion string `json:"robot_version,omitempty"` // empty when the robot has not reported it
	Rooms        int    `json:"rooms"`
}

// VacuumRoomsSyncResult is the result of vacuum.roomsync. It never contains a
// credential or a token.
type VacuumRoomsSyncResult struct {
	Maps    []VacuumSyncedMap `json:"maps"`
	Added   int               `json:"added"`
	Updated int               `json:"updated"`
	Stale   int               `json:"stale"` // regions no longer listed by the cloud, now marked stale
}

// VacuumRoomNameParams names a learned region.
type VacuumRoomNameParams struct {
	Robot    string `json:"robot,omitempty"`
	RegionID string `json:"region_id"`
	Name     string `json:"name"`
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
