package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// Clean this room (#765, #684 PR 4). The room's vacuum block and the
// POST /htmx/vacuum/action `room_id` branch share one resolution, roomCleanPlan,
// so what the button promises and what the server then does cannot differ. Both
// read the daemon's cached robot state and SQLite only; no robot is contacted
// until vacuum.clean itself.

// roomCleanPlan is what "clean this room" would do right now.
type roomCleanPlan struct {
	Robot    myhome.VacuumRobot
	IDs      []string // region ids to send to vacuum.clean; all on one map of one robot
	Areas    []string // labels of every region mapped to the room, stale ones marked
	Assigned bool     // at least one region (stale or not) is mapped to the room
	Reason   string   // why the room cannot be cleaned now; "" when it can
	Shown    bool     // a robot is configured
}

// regionLabel names a region for display.
func regionLabel(reg myhome.VacuumRegion) string {
	name := reg.Name
	if name == "" {
		name = "region " + reg.ID
	}
	if reg.Stale {
		name += " (robot map changed)"
	}
	return name
}

// sharesIdentifier reports whether vacuum.clean, which addresses a room by id or
// by name only, could also match other as the region reg.ID designates. Robot
// region ids can collide across maps, so this is checked rather than assumed.
func sharesIdentifier(reg, other myhome.VacuumRegion) bool {
	return other.ID == reg.ID || strings.EqualFold(other.Name, reg.ID)
}

// planRoomClean resolves roomID against every robot's regions, per (robot, map,
// region): a region id alone is never a key.
func (h *HTMXHandler) planRoomClean(roomID string) roomCleanPlan {
	var p roomCleanPlan
	res, err := callVacuum(h.ctx, myhome.VacuumList, nil)
	if err != nil {
		return p
	}
	list, ok := res.(*myhome.VacuumListResult)
	if !ok || len(list.Robots) == 0 {
		return p
	}
	p.Shown = true

	type group struct {
		robot myhome.VacuumRobot
		mapID string
		regs  []myhome.VacuumRegion
		all   []myhome.VacuumRegion // every region of the robot, to detect id collisions
	}
	var groups []*group
	for _, r := range list.Robots {
		rooms, err := callVacuum(h.ctx, myhome.VacuumRooms, &myhome.VacuumParams{Robot: r.ID})
		if err != nil {
			continue
		}
		rr, ok := rooms.(*myhome.VacuumRoomsResult)
		if !ok {
			continue
		}
		for _, reg := range rr.Regions {
			if reg.RoomID != roomID {
				continue
			}
			p.Assigned = true
			p.Areas = append(p.Areas, regionLabel(reg))
			if reg.Stale {
				continue // a stale area is never cleaned
			}
			var g *group
			for _, c := range groups {
				if c.robot.ID == r.ID && c.mapID == reg.MapID {
					g = c
				}
			}
			if g == nil {
				g = &group{robot: r, mapID: reg.MapID, all: rr.Regions}
				groups = append(groups, g)
			}
			g.regs = append(g.regs, reg)
		}
	}
	sort.Strings(p.Areas)

	switch {
	case !p.Assigned:
		return p
	case len(groups) == 0:
		p.Reason = "Areas are out of date on the robot (run myhome ctl vacuum rooms sync)"
		return p
	case len(groups) > 1:
		p.Reason = "Areas are on different robot maps"
		return p
	}
	g := groups[0]
	p.Robot = g.robot
	for _, reg := range g.regs {
		for _, other := range g.all {
			if other.Stale || (other.MapID == reg.MapID && other.ID == reg.ID) {
				continue
			}
			if sharesIdentifier(reg, other) {
				p.Reason = fmt.Sprintf("Area id %s is also used on another robot map, so the robot cannot tell them apart", reg.ID)
				return p
			}
		}
		p.IDs = append(p.IDs, reg.ID)
	}

	ctl := controlsFor(deriveVacuumState(g.robot.Connected, g.robot.Status))
	switch {
	case !g.robot.Connected:
		p.Reason = "Robot offline"
		if g.robot.Host != "" {
			p.Reason += " (last seen at " + g.robot.Host + ")"
		}
	case !ctl.CleanAll:
		p.Reason = "Robot busy"
		if st := g.robot.Status; st != nil {
			if t := targetNames(st.Regions, g.all); len(t) > 0 && st.Cycle != "" && st.Cycle != "none" {
				p.Reason += ": cleaning " + strings.Join(t, ", ")
			} else {
				p.Reason += ": " + myhome.VacuumPhaseText(st.Phase)
			}
		}
	}
	return p
}

// roomVacuum builds the room page's vacuum block. notice and errText carry the
// outcome of a clean just requested from this block.
func (h *HTMXHandler) roomVacuum(roomID, roomName, notice, errText string) roomVacuumView {
	if roomName == "" {
		roomName = roomID
	}
	v := roomVacuumView{RoomID: roomID, RoomName: roomName, Notice: notice, Error: errText}
	p := h.planRoomClean(roomID)
	if !p.Shown {
		return v
	}
	v.Show, v.Areas, v.Reason = true, p.Areas, p.Reason
	v.CanClean = p.Assigned && p.Reason == ""
	if v.CanClean {
		vals, _ := json.Marshal(map[string]string{"robot": p.Robot.ID, "command": "clean-rooms", "room_id": roomID})
		v.Vals = string(vals)
	}
	return v
}

// roomName is the display name of a room, looked up like the room page does.
func (h *HTMXHandler) roomName(roomID string) (string, error) {
	mh, err := myhome.Methods(myhome.TemperatureList)
	if err != nil {
		return "", fmt.Errorf("rooms are not enabled on this daemon")
	}
	res, err := mh.ActionE(h.ctx, mh.Signature.NewParams())
	rooms, ok := res.(*myhome.TemperatureRoomList)
	if err != nil || !ok {
		return "", fmt.Errorf("couldn't load the rooms")
	}
	rc, known := (*rooms)[roomID]
	if !known {
		return "", fmt.Errorf("unknown room %q", roomID)
	}
	if rc.Name == "" {
		return roomID, nil
	}
	return rc.Name, nil
}

// cleanRoom serves the `room_id` branch of POST /htmx/vacuum/action. The client's
// `room` fields were never read: the regions are resolved here, now, and the
// robot's connection and idleness are re-checked (the block may be 5 s old, or a
// confirm may have sat open). The response is the room's vacuum block.
func (h *HTMXHandler) cleanRoom(w http.ResponseWriter, robot, roomID string) {
	var notice string
	name, err := h.roomName(roomID)
	if err == nil {
		err = h.startRoomClean(robot, roomID, name, &notice)
	}
	var errText string
	if err != nil {
		h.log.Info("Room clean failed", "robot", robot, "room", roomID, "error", err.Error())
		errText = "Clean failed: " + err.Error()
	}
	if name == "" {
		name = roomID
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := roomTmpl.ExecuteTemplate(w, "room-vacuum", h.roomVacuum(roomID, name, notice, errText)); err != nil {
		h.log.Error(err, "failed to render room vacuum block")
	}
}

func (h *HTMXHandler) startRoomClean(robot, roomID, name string, notice *string) error {
	p := h.planRoomClean(roomID)
	switch {
	case !p.Shown:
		return fmt.Errorf("no vacuum robot is configured")
	case !p.Assigned:
		return fmt.Errorf("no robot area is assigned to this room")
	case p.Reason != "":
		return fmt.Errorf("%s, nothing was started", p.Reason)
	case robot != p.Robot.ID && robot != p.Robot.Name:
		return fmt.Errorf("this room's areas are on robot %q, not %q", p.Robot.Name, robot)
	}
	res, err := callVacuum(h.ctx, myhome.VacuumClean, &myhome.VacuumCleanParams{Robot: p.Robot.ID, Rooms: p.IDs})
	if err != nil {
		return err
	}
	*notice = "Cleaning " + name
	if cr, ok := res.(*myhome.VacuumCleanResult); ok && len(cr.Attempts) > 0 &&
		cr.Attempts[len(cr.Attempts)-1].Outcome == myhome.VacuumCleanNoReport {
		*notice = "Sent; the robot has not confirmed yet"
	}
	return nil
}
