package scripts

import "github.com/asnowfix/home-automation/pkg/shelly/script"

// scriptMinifyOverrides maps an embedded script's basename to the top-level
// symbol names that must remain reachable as real globals if that script is
// minified with esbuild's top-level mangling enabled (opt-in; see
// pkg/shelly/script.MinifyOptions.MangleTopLevel). This registry is the
// concrete answer to "make the preserve-list configurable per script, not
// hardcoded to pool-pump": add an entry here, not in pkg/shelly/script
// (which must stay generic -- see the Three-Tier Layer Rule in CLAUDE.md).
//
// Two categories per entry:
//
//   - PreserveGlobals: names invoked BY NAME from outside the script's own
//     JS execution -- concretely, a Shelly Schedule job whose "code" field
//     is a bare call like "handleMorningStart()", evaluated later via
//     script.eval. If MangleTopLevel renames these away without
//     re-exporting them, the schedule silently stops working (the eval
//     throws "not defined", which nothing here catches -- it fails on
//     the device, not at build time). Get this list right.
//   - DebugExports: additional names kept reachable purely so a developer
//     can inspect them live via Script.Eval while debugging on real
//     hardware (e.g. STATE, CONFIG). This is a deliberate trade-off,
//     documented on MinifyOptions.DebugExports: real debuggability gained,
//     in exchange for a few extra bytes and a small global-namespace
//     collision risk with other scripts on the same device. Left empty
//     by default; a developer opts in per investigation, not permanently.
//
// A script with no entry here gets the zero value (empty PreserveGlobals
// and DebugExports) from MinifyOptionsFor, which is a SAFE default -- it
// just means nothing is protected. Any script that gains a
// Schedule.Create-by-name call (grep for `code:` in the .js files under
// this package to check) must get an entry here before its build path is
// ever switched to esbuild with MangleTopLevel.
var scriptMinifyOverrides = map[string]script.MinifyOptions{
	// pool-pump.js: five Schedule jobs (daily-check, morning-start,
	// evening-stop, night-start, night-stop) each eval one of these by
	// name. NOTE: the four-name list quoted in earlier investigation notes
	// (issue #421 handover) omitted handleDailyCheck -- verified against
	// the current source (grep for `code: '` in pool-pump.js) and included
	// here. pool-pump.js is owned by PR #426 as of this writing and is
	// deliberately NOT switched to esbuild by this change; this entry
	// exists so the registry is correct and ready whenever that switch is
	// made.
	"pool-pump.js": {
		PreserveGlobals: []string{
			"handleDailyCheck",
			"handleMorningStart",
			"handleEveningStop",
			"handleNightStart",
			"handleNightStop",
		},
		// Names most useful for live Script.Eval debugging during the
		// #421 investigation. Off by default (DebugExports is only
		// consulted when a caller explicitly asks MinifyOptionsFor to
		// merge it in -- see includeDebugExports below); listed here so
		// the option exists without editing this file again mid-incident.
		DebugExports: []string{"STATE", "CONFIG", "TASK_QUEUE"},
	},
	// garden.js: two Schedule jobs (handlePlan, handleWateringStart).
	"garden.js": {
		PreserveGlobals: []string{
			"handlePlan",
			"handleWateringStart",
		},
	},
	// watchdog.js, prometheus-metrics.js, and every other embedded script
	// currently have no Schedule.Create-by-name calls (verified by
	// grepping for `code:` across internal/shelly/scripts/*.js), so no
	// entry is needed -- an empty PreserveGlobals list is correct, not an
	// oversight.
}

// MinifyOptionsFor returns the MinifyOptions to use for top-level mangling
// of the given embedded script (looked up by basename, e.g. "pool-pump.js"),
// merging the caller-supplied engine/MangleTopLevel/includeDebugExports
// choice with this script's registered preserve/debug lists.
//
// includeDebugExports controls whether the registered DebugExports are
// actually applied -- keep it false for routine/automatic minification
// (the daemon's auto-setup path) and set it true only when a developer is
// deliberately trading a few bytes for live-debuggability on a specific
// investigation. This mirrors the "opt-in, default OFF" requirement that
// also governs MangleTopLevel itself.
func MinifyOptionsFor(scriptName string, engine script.Engine, mangleTopLevel bool, includeDebugExports bool) script.MinifyOptions {
	base := scriptMinifyOverrides[scriptName] // zero value if absent: no preserve/debug names
	opts := script.MinifyOptions{
		Engine:          engine,
		MangleTopLevel:  mangleTopLevel,
		PreserveGlobals: append([]string(nil), base.PreserveGlobals...),
	}
	if includeDebugExports {
		opts.DebugExports = append([]string(nil), base.DebugExports...)
	}
	return opts
}
