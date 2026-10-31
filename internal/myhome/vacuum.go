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
	Mission        int         `json:"mission,omitempty"` // mission counter (nMssn)
	LastCommand    string      `json:"last_command,omitempty"`
	Regions        []string    `json:"regions,omitempty"` // ids of the rooms/zones the last command asked for; empty for a whole-home clean
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
	// (empty until the robot has reported). Information only: for a cloud
	// region it normally differs from MapVersion (the robot's moves on after
	// every clean, the cloud's stays at the last map confirmed in the app), and
	// a room clean names MapVersion, which the robot accepts (measured
	// 2026-10-30, #628).
	RobotMapVersion string `json:"robot_map_version,omitempty"`
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

// Outcomes of one room-clean attempt (VacuumCleanAttempt.Outcome).
const (
	VacuumCleanRunning  = "running"   // the robot reached phase run or cycle clean
	VacuumCleanAborted  = "aborted"   // back on the dock, cycle none, with an error, without ever running
	VacuumCleanNoReport = "no-report" // the watch window ended with neither: acknowledged, outcome unknown
)

// VacuumCleanAttempt is one start command sent for a room clean.
type VacuumCleanAttempt struct {
	MapVersion string `json:"map_version"` // the user_pmapv_id named in the command
	Outcome    string `json:"outcome"`
	Error      int    `json:"error,omitempty"` // cleanMissionStatus.error on an abort
}

// VacuumCleanResult is the result of vacuum.clean: the attempts made (one, or
// two when the first aborted at once and the other known version was tried).
type VacuumCleanResult struct {
	OK       bool                 `json:"ok"`
	MapID    string               `json:"map_id"`
	Attempts []VacuumCleanAttempt `json:"attempts"`
}

// VacuumErrorHint explains a cleanMissionStatus.error code, or returns "" when
// there is nothing to add to the bare number. iRobot documents none of them.
func VacuumErrorHint(code int) string {
	if code == 224 {
		return "error 224: the robot opened the mission and aborted at once; seen when a room clean names a map version the robot does not accept. Rooms from the iRobot cloud are cleaned with the cloud map version"
	}
	return ""
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
