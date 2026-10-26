package roomba

import (
	"encoding/json"
	"sort"
)

// Phases reported in cleanMissionStatus.phase (observed on an i7 unless noted).
const (
	PhaseCharge    = "charge"    // on the dock, charging
	PhaseRun       = "run"       // cleaning
	PhaseStop      = "stop"      // paused, or stopped off the dock
	PhaseHmUsrDock = "hmUsrDock" // returning to the dock on user request
	PhaseHmMidMsn  = "hmMidMsn"  // returning mid-mission to recharge (dorita980 docs)
	PhaseHmPostMsn = "hmPostMsn" // returning after the mission (dorita980 docs)
	PhaseStuck     = "stuck"     // needs help (dorita980 docs)
	PhaseEvac      = "evac"      // emptying into the Clean Base (dorita980 docs)
)

// Map is one persistent map: its id and its version token. The version changes
// whenever the map is edited in the app, which makes stored region ids stale.
type Map struct {
	ID      string `json:"pmap_id"`
	Version string `json:"user_pmapv_id"`
}

// Region is a room ("rid") or zone ("zid") of a persistent map, as a room-clean
// command addresses it.
type Region struct {
	ID   string `json:"region_id"`
	Type string `json:"type"`
}

// LastCommand is the robot's echo of the last command it accepted, from any source.
type LastCommand struct {
	Command   string   `json:"command"`
	Time      int64    `json:"time"`
	Initiator string   `json:"initiator"`
	MapID     string   `json:"pmap_id,omitempty"`
	MapVer    string   `json:"user_pmapv_id,omitempty"`
	Regions   []Region `json:"regions,omitempty"`
}

// Status is the subset of the robot's reported state that callers act on.
type Status struct {
	Name           string      `json:"name"`
	Battery        int         `json:"battery"`
	BinFull        bool        `json:"bin_full"`
	BinPresent     bool        `json:"bin_present"`
	Phase          string      `json:"phase"`
	Cycle          string      `json:"cycle"`
	Error          int         `json:"error"`
	NotReady       int         `json:"not_ready"`
	MissionMinutes int         `json:"mission_minutes"`
	Initiator      string      `json:"initiator"`
	Maps           []Map       `json:"maps"`
	LastCommand    LastCommand `json:"last_command"`
	Firmware       string      `json:"firmware"`
}

// state accumulates the robot's reported shadow. The robot pushes partial
// deltas, and only when something changes, so every update is merged key by key.
type state map[string]json.RawMessage

// merge applies one pushed message ({"state":{"reported":{...}}}).
// It reports whether the message was a state update at all.
func (s state) merge(payload []byte) bool {
	var msg struct {
		State struct {
			Reported map[string]json.RawMessage `json:"reported"`
		} `json:"state"`
	}
	if json.Unmarshal(payload, &msg) != nil || msg.State.Reported == nil {
		return false
	}
	for k, v := range msg.State.Reported {
		s[k] = v
	}
	return true
}

// ready reports whether the fields Status depends on have all arrived.
func (s state) ready() bool {
	_, a := s["batPct"]
	_, b := s["cleanMissionStatus"]
	return a && b
}

func (s state) lastCommand() LastCommand {
	var lc struct {
		LastCommand
		Time *int64 `json:"time"` // null right after provisioning
	}
	_ = json.Unmarshal(s["lastCommand"], &lc)
	if lc.Time != nil {
		lc.LastCommand.Time = *lc.Time
	}
	return lc.LastCommand
}

func (s state) status() Status {
	var st Status
	_ = json.Unmarshal(s["name"], &st.Name)
	_ = json.Unmarshal(s["batPct"], &st.Battery)
	_ = json.Unmarshal(s["softwareVer"], &st.Firmware)

	var bin struct {
		Full    bool `json:"full"`
		Present bool `json:"present"`
	}
	_ = json.Unmarshal(s["bin"], &bin)
	st.BinFull, st.BinPresent = bin.Full, bin.Present

	var cms struct {
		Phase     string `json:"phase"`
		Cycle     string `json:"cycle"`
		Error     int    `json:"error"`
		NotReady  int    `json:"notReady"`
		MssnM     int    `json:"mssnM"`
		Initiator string `json:"initiator"`
	}
	_ = json.Unmarshal(s["cleanMissionStatus"], &cms)
	st.Phase, st.Cycle, st.Error, st.NotReady = cms.Phase, cms.Cycle, cms.Error, cms.NotReady
	st.MissionMinutes, st.Initiator = cms.MssnM, cms.Initiator

	// pmaps is a list of single-entry objects: [{"<pmap_id>":"<user_pmapv_id>"}]
	var pmaps []map[string]string
	_ = json.Unmarshal(s["pmaps"], &pmaps)
	for _, m := range pmaps {
		for id, ver := range m {
			st.Maps = append(st.Maps, Map{ID: id, Version: ver})
		}
	}
	sort.Slice(st.Maps, func(i, j int) bool { return st.Maps[i].ID < st.Maps[j].ID })

	st.LastCommand = s.lastCommand()
	return st
}
