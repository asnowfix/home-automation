package scripts

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// wrapScheduleCallCallRe extracts the handlerCall argument from every
// wrapScheduleCall('...') / wrapScheduleCall("...") call site in a script's
// source, so this test tracks whatever handler names are actually registered
// instead of a hand-maintained list that can silently go stale.
var wrapScheduleCallCallRe = regexp.MustCompile(`wrapScheduleCall\(\s*['"]([^'"]+)['"]\s*\)`)

// quotedHandlerNameRe extracts every single-quoted "handleXxx()" literal from
// a script's source. In pool-pump.js this finds exactly the four handler
// names that participate in onWindowJobs()'s/updateScheduleMode()'s
// by-name field renaming (handleMorningStart, handleEveningStop,
// handleNightStart, handleNightStop) -- NOT the fifth pool-pump schedule,
// handleDailyCheck(), which is deliberately never written as a quoted
// 'handleDailyCheck()' literal: it doesn't participate in window-mode
// toggling, so verifySchedules() only ever needs to detect its *presence*
// via the truncated, unquoted prefix "handleDaily" (see pool-pump.js), never
// an exact quoted match. Do not "fix" this regex to also match
// handleDailyCheck() -- there is nothing in the source for it to match; the
// authoritative, complete list of all five pool-pump handler names lives in
// Go now (getDesiredSchedules(), internal/myhome/shelly/script/pool.go,
// #524/#528) and is checked in full there, not here -- see
// TestScheduleHandlerNamesDontCollide in
// internal/myhome/shelly/script/pool_test.go.
var quotedHandlerNameRe = regexp.MustCompile(`'(handle[A-Za-z]+\(\))'`)

// registeredHandlerCalls reads path and returns every handlerCall string
// passed to wrapScheduleCall() in it, in source order. Fails the test if the
// script doesn't call wrapScheduleCall at all -- that would mean either the
// script no longer registers any schedule.eval jobs (fine, caller should
// only invoke this for scripts known to use the pattern) or #480's wrapper
// silently stopped being used.
func registeredHandlerCalls(t *testing.T, path string) []string {
	t.Helper()
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	matches := wrapScheduleCallCallRe.FindAllStringSubmatch(string(buf), -1)
	if len(matches) == 0 {
		t.Fatalf("no wrapScheduleCall(...) call sites found in %s", path)
	}
	calls := make([]string, len(matches))
	for i, m := range matches {
		calls[i] = m[1]
	}
	return calls
}

// quotedHandlerNames reads path and returns every distinct 'handleXxx()'
// literal found in it, in source order of first appearance. Fails the test
// if none are found -- see quotedHandlerNameRe.
func quotedHandlerNames(t *testing.T, path string) []string {
	t.Helper()
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	matches := quotedHandlerNameRe.FindAllStringSubmatch(string(buf), -1)
	if len(matches) == 0 {
		t.Fatalf("no 'handleXxx()' literals found in %s", path)
	}
	seen := map[string]bool{}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	return names
}

// scheduleWrapBoilerplate is wrapScheduleCall()'s fixed template with the
// handlerCall placeholder removed -- i.e. everything that surrounds every
// registered handler call verbatim, in both pool-pump.js and garden.js. It is
// asserted equal to the live source in TestScheduleWrap_BoilerplateMatchesSource
// below so this constant cannot silently drift from the two scripts.
const scheduleWrapBoilerplate = "(function(){try{" + "}catch(e){log('schedule handler error:',e)}})()"

// assertNoHandlerNameCollisions is the #480 invariant the substring-match
// fix in onWindowJobs()/updateScheduleMode()/verifySchedules() (pool-pump.js)
// and updatePlanSchedule()/verifySchedules() (garden.js) depends on: reading a
// (possibly wrapped) `code` field back and matching it against a
// handler name by containment only identifies the right job if no handler
// name is ever a substring of another, and if the wrapper's own boilerplate
// text never happens to contain a handler name. Both are true today only
// because every handler call ends in "()" and the names themselves don't
// nest -- exactly the kind of fact that quietly breaks when someone adds
// e.g. handleMorningStartDelayed() next to handleMorningStart(). This turns
// that invariant into something CI enforces instead of a comment.
func assertNoHandlerNameCollisions(t *testing.T, scriptName string, handlerCalls []string) {
	t.Helper()

	seen := map[string]bool{}
	for _, h := range handlerCalls {
		seen[h] = true
	}

	unique := make([]string, 0, len(seen))
	for h := range seen {
		unique = append(unique, h)
	}

	for i, a := range unique {
		for j, b := range unique {
			if i == j {
				continue
			}
			if strings.Contains(a, b) {
				t.Errorf("%s: handler name %q contains %q as a substring -- "+
					"substring-based Schedule.List matching (see wrapScheduleCall callers) "+
					"cannot tell these two handlers' jobs apart", scriptName, a, b)
			}
		}
		if strings.Contains(scheduleWrapBoilerplate, a) {
			t.Errorf("%s: wrapper boilerplate itself contains handler name %q -- "+
				"every job's code field would match this handler regardless of which "+
				"handler it actually calls", scriptName, a)
		}
	}

	if len(unique) < 2 {
		t.Fatalf("%s: expected at least 2 distinct registered handler names to make this check "+
			"meaningful, got %d (%v) -- did extraction break?", scriptName, len(unique), unique)
	}
}

// TestScheduleWrap_PoolPumpHandlerNamesDontCollide is the #480 no-collision
// guard for the four pool-pump.js handler names that appear as quoted
// 'handleXxx()' literals: handleMorningStart, handleEveningStop,
// handleNightStart, handleNightStop -- the ones onWindowJobs()'s and
// updateScheduleMode()'s by-name field renaming compare a live job's `code`
// against. #524: pool-pump.js no longer registers these jobs itself
// (createSchedules() removed as dead code -- see
// internal/myhome/shelly/script/pool.go), so the handler names are extracted
// from the source rather than from wrapScheduleCall(...) call sites.
//
// This intentionally does NOT cover handleDailyCheck() -- see the comment on
// quotedHandlerNameRe for why that name is never quoted this way in the
// source. The full five-handler collision check, against the actual
// authority for "which handlers exist" (getDesiredSchedules()), lives in
// TestScheduleHandlerNamesDontCollide, internal/myhome/shelly/script/pool_test.go
// (#528). A prior version of this test's doc comment claimed to cover five
// handlers while quotedHandlerNameRe only ever matched four -- fixed here by
// being explicit about the four this extraction method actually finds.
func TestScheduleWrap_PoolPumpHandlerNamesDontCollide(t *testing.T) {
	handlers := quotedHandlerNames(t, poolPumpScriptPath)

	const wantCount = 4
	if len(handlers) != wantCount {
		t.Fatalf("expected exactly %d quoted 'handleXxx()' literals in %s, got %d (%v) -- "+
			"if this changed, re-check whether it's still true that handleDailyCheck() never "+
			"appears quoted this way (see quotedHandlerNameRe), and update the five-handler "+
			"authority check in internal/myhome/shelly/script/pool_test.go if the handler set itself changed",
			wantCount, poolPumpScriptPath, len(handlers), handlers)
	}

	assertNoHandlerNameCollisions(t, "pool-pump.js", handlers)
}

// TestScheduleWrap_GardenHandlerNamesDontCollide is the same guard for
// garden.js's two schedule.eval jobs (handlePlan, handleWateringStart).
func TestScheduleWrap_GardenHandlerNamesDontCollide(t *testing.T) {
	handlers := registeredHandlerCalls(t, gardenScriptPath)
	assertNoHandlerNameCollisions(t, "garden.js", handlers)
}

// TestScheduleWrap_BoilerplateMatchesSource keeps scheduleWrapBoilerplate
// (used above to check the wrapper template itself doesn't accidentally
// contain a handler name) from silently drifting away from what
// wrapScheduleCall() actually emits. If the wrapper changes, this fails
// until the Go constant -- and the collision check it backs -- is updated to
// match. #524: pool-pump.js dropped its own wrapScheduleCall() (dead code,
// createSchedules() lost its only caller in #504) -- only garden.js still
// wraps schedule payloads this way, so only garden.js is checked here.
func TestScheduleWrap_BoilerplateMatchesSource(t *testing.T) {
	for _, path := range []string{gardenScriptPath} {
		buf, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", path, err)
		}
		src := string(buf)
		re := regexp.MustCompile(`function wrapScheduleCall\(handlerCall\) \{\s*return "([^"]*)" \+ handlerCall \+ "([^"]*)";`)
		m := re.FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("%s: could not locate wrapScheduleCall()'s return statement -- "+
				"update this test's regex if the implementation changed shape", path)
		}
		gotBoilerplate := m[1] + m[2]
		if gotBoilerplate != scheduleWrapBoilerplate {
			t.Errorf("%s: wrapScheduleCall() boilerplate is %q, test constant scheduleWrapBoilerplate is %q -- "+
				"update the constant (and re-check for handler-name collisions against the new text)",
				path, gotBoilerplate, scheduleWrapBoilerplate)
		}
	}
}
