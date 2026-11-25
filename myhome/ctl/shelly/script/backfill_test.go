package script

import (
	"strings"
	"testing"

	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
)

func TestFormatBackfillReport(t *testing.T) {
	sha := func(c byte) string { return strings.Repeat(string(c), 40) }
	r := &mhscript.DeviceScriptsReport{
		Device: "dev",
		Scripts: []mhscript.ScriptUpdate{
			{Script: "watchdog.js", Outcome: mhscript.OutcomeMarked, OldVersion: "none", NewVersion: sha('a')},
			{Script: "heater.js", Outcome: mhscript.OutcomeAlreadyMarked, OldVersion: sha('b'), Reason: "a marker exists; left alone"},
			{Script: "pool-pump.js", Outcome: mhscript.OutcomeSkipped, Reason: "pool-pump.js is guarded: not read, no marker written"},
		},
	}
	want := "dev: watchdog.js marked (none -> aaaaaaa)\n" +
		"dev: heater.js already-marked (bbbbbbb): a marker exists; left alone\n" +
		"dev: pool-pump.js skipped: pool-pump.js is guarded: not read, no marker written\n"
	if got := formatBackfillReport(r, ""); got != want {
		t.Errorf("formatBackfillReport =\n%s\nwant\n%s", got, want)
	}
}
