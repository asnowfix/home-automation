package ui

import (
	"fmt"

	"github.com/asnowfix/home-automation/myhome/homedrive"
)

// guardLines turns the schema 2.1 guards into short human lines for the panel.
// Codes and counts only (the contract carries no path); an unknown reason from
// a newer producer gets a generic line rather than the raw code.
func guardLines(g *homedrive.Guards) []string {
	if g == nil {
		return nil
	}
	var lines []string
	if r := g.LocalRoot; r != nil {
		lines = append(lines, "Sync paused: "+rootReasonText(r.Reason))
	}
	if d := g.BisyncDeletes; d != nil {
		lines = append(lines, "Bisync deletes held: "+deletesReasonText(d))
	}
	return lines
}

func rootReasonText(reason string) string {
	switch reason {
	case homedrive.RootReasonMissing:
		return "local folder missing"
	case homedrive.RootReasonMarkerMissing:
		return "local folder is not the synced tree (marker missing)"
	case homedrive.RootReasonIdentityMismatch:
		return "local folder is not the one that was synced"
	case homedrive.RootReasonUnreadable:
		return "local folder unreadable"
	}
	return "local folder blocked"
}

func deletesReasonText(d *homedrive.DeletesGuard) string {
	switch d.Reason {
	case homedrive.DeletesReasonThreshold:
		return fmt.Sprintf("%s planned, limit %s", group(d.Planned), group(d.Threshold))
	case homedrive.DeletesReasonEmptyLocal:
		return fmt.Sprintf("local side is empty, %s planned", group(d.Planned))
	case homedrive.DeletesReasonEmptyRemote:
		return fmt.Sprintf("Drive side is empty, %s planned", group(d.Planned))
	}
	return fmt.Sprintf("%s planned", group(d.Planned))
}
