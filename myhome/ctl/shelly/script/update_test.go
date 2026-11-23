package script

import (
	"strings"
	"testing"

	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
)

// formatReport prints one greppable line per device and script, prefixed with
// the device name (#863).
func TestFormatReport(t *testing.T) {
	sha := func(c byte) string { return strings.Repeat(string(c), 40) }
	r := &mhscript.DeviceScriptsReport{
		Device: "pump",
		Scripts: []mhscript.ScriptUpdate{
			{Script: "watchdog.js", Outcome: mhscript.OutcomeUpToDate, NewVersion: sha('a'), Reason: "Script version is the same"},
			{Script: "heater.js", Outcome: mhscript.OutcomeUpdated, OldVersion: "none", NewVersion: sha('b')},
			{Script: "pool-pump.js", Outcome: mhscript.OutcomeRefused, OldVersion: sha('c'), NewVersion: sha('d'), Reason: "pool-pump.js is guarded"},
			{Script: "blu-listener.js", Outcome: mhscript.OutcomeFailed, Reason: "upload did not complete: timeout", Backup: "/x/y.bak.js"},
		},
	}
	got := formatReport(r, "")
	want := []string{
		"pump: watchdog.js up-to-date (aaaaaaa)\n",
		"pump: heater.js updated (none -> bbbbbbb)\n",
		"pump: pool-pump.js refused: pool-pump.js is guarded\n",
		"pump: blu-listener.js failed: upload did not complete: timeout [previous code saved to /x/y.bak.js]\n",
	}
	if got != strings.Join(want, "") {
		t.Errorf("formatReport =\n%s\nwant\n%s", got, strings.Join(want, ""))
	}

	if got := formatReport(&mhscript.DeviceScriptsReport{Device: "d"}, "heater.js"); got != "d: heater.js is not loaded (skipped)\n" {
		t.Errorf("target not loaded = %q", got)
	}
}
