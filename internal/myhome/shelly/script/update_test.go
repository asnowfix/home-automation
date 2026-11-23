package script

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr/testr"
)

// Tests for UpdateDeviceScripts, the logic behind `script update` (#863).
// They run against fakeUploadDevice (ops_test.go); no device is touched.

// localScripts writes the given scripts to a temporary --local-scripts-dir so
// a test controls exactly what "the packaged version" is.
func localScripts(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, code := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// withFastSettle shrinks the guarded-script settle watch for one test.
func withFastSettle(t *testing.T) {
	t.Helper()
	origChecks, origDelay := guardSettleChecks, guardSettleDelay
	guardSettleChecks = 2
	guardSettleDelay = time.Millisecond
	t.Cleanup(func() {
		guardSettleChecks = origChecks
		guardSettleDelay = origDelay
	})
}

func outcomes(r *DeviceScriptsReport) map[string]ScriptUpdate {
	m := map[string]ScriptUpdate{}
	for _, s := range r.Scripts {
		m[s.Script] = s
	}
	return m
}

func mustUpdate(t *testing.T, dev types.Device, opts UpdateOptions) map[string]ScriptUpdate {
	t.Helper()
	r, err := UpdateDeviceScripts(context.Background(), testr.New(t), types.ChannelDefault, dev, opts)
	if err != nil {
		t.Fatalf("UpdateDeviceScripts: %v", err)
	}
	return outcomes(r)
}

func resetCalls(f *fakeUploadDevice) { f.calls = map[string]int{} }

// The acceptance criterion of #863: running the push twice on an unchanged
// fleet reports up-to-date for every script on the second run, and the second
// run sends no code.
func TestUpdateDeviceScripts_SecondRunIsUpToDate(t *testing.T) {
	withFastRetry(t, 1)
	dir := localScripts(t, map[string]string{
		"watchdog.js":        "// watchdog v2\n",
		"status-listener.js": "// status-listener v2\n",
	})
	dev := newFakeUploadDevice("dev-863-twice")
	dev.scripts[1] = "watchdog.js"
	dev.scripts[2] = "status-listener.js"
	dev.nextID = 2
	dev.kv["script/watchdog.js"] = ScriptVersion([]byte("// watchdog v1\n")) // an older version
	// status-listener.js has no marker at all (first push on this device)
	opts := UpdateOptions{LocalDir: dir}

	first := mustUpdate(t, dev, opts)
	for _, name := range []string{"watchdog.js", "status-listener.js"} {
		if got := first[name].Outcome; got != OutcomeUpdated {
			t.Errorf("first run: %s = %s (%s), want %s", name, got, first[name].Reason, OutcomeUpdated)
		}
	}
	if got := first["status-listener.js"].OldVersion; got != "none" {
		t.Errorf("first run: status-listener.js old version = %q, want none", got)
	}

	resetCalls(dev)
	second := mustUpdate(t, dev, opts)
	for _, name := range []string{"watchdog.js", "status-listener.js"} {
		if got := second[name].Outcome; got != OutcomeUpToDate {
			t.Errorf("second run: %s = %s (%s), want %s", name, got, second[name].Reason, OutcomeUpToDate)
		}
	}
	for _, m := range []string{"Script.Stop", "Script.PutCode", "Script.Create", "KVS.Set"} {
		if n := dev.calls[m]; n != 0 {
			t.Errorf("second run called %s %d time(s); an unchanged fleet must receive no code", m, n)
		}
	}
}

// Before #863 a confirmation-gap upload (every chunk acknowledged, SetConfig
// not confirmed) skipped the marker write, so the next push uploaded the same
// code again.
func TestUpdateDeviceScripts_ConfirmationGapStillRecordsMarker(t *testing.T) {
	withFastRetry(t, 1)
	dir := localScripts(t, map[string]string{"watchdog.js": "// watchdog v2\n"})
	inner := newFakeUploadDevice("dev-863-gap")
	inner.scripts[1] = "watchdog.js"
	inner.nextID = 1
	dev := &failingCallDevice{fakeUploadDevice: inner, failMethod: "Script.SetConfig"}

	first := mustUpdate(t, dev, UpdateOptions{LocalDir: dir})
	if got := first["watchdog.js"].Outcome; got == OutcomeFailed {
		t.Fatalf("a confirmation gap is not a failure: %s", first["watchdog.js"].Reason)
	}
	if got, want := inner.kv["script/watchdog.js"], ScriptVersion([]byte("// watchdog v2\n")); got != want {
		t.Fatalf("marker after a confirmation-gap upload = %q, want %q", got, want)
	}

	resetCalls(inner)
	second := mustUpdate(t, dev, UpdateOptions{LocalDir: dir})
	if got := second["watchdog.js"].Outcome; got != OutcomeUpToDate {
		t.Errorf("second run = %s, want up-to-date", got)
	}
	if inner.calls["Script.PutCode"] != 0 {
		t.Errorf("second run re-sent code after a confirmation gap")
	}
}

// A marker that could not be recorded makes the next push upload again, so
// it is reported as a failure with its reason, not as a success.
func TestUpdateDeviceScripts_MarkerWriteFailureIsReported(t *testing.T) {
	withFastRetry(t, 1)
	dir := localScripts(t, map[string]string{"watchdog.js": "// watchdog v2\n"})
	dev := newFakeUploadDevice("dev-863-kvsset")
	dev.scripts[1] = "watchdog.js"
	dev.nextID = 1
	dev.kvSetErr = errors.New("timeout waiting for response")

	r, err := UpdateDeviceScripts(context.Background(), testr.New(t), types.ChannelDefault, dev, UpdateOptions{LocalDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	s := outcomes(r)["watchdog.js"]
	if s.Outcome != OutcomeFailed || !strings.Contains(s.Reason, "version marker") {
		t.Errorf("outcome = %s (%q), want failed naming the version marker", s.Outcome, s.Reason)
	}
	if len(r.Failed()) != 1 {
		t.Errorf("Failed() = %d, want 1", len(r.Failed()))
	}
}

func TestUpdateDeviceScripts_DryRunChangesNothing(t *testing.T) {
	dir := localScripts(t, map[string]string{
		"watchdog.js":  "// watchdog v2\n",
		"pool-pump.js": "// pump v2\n",
		"heater.js":    "// heater v1\n",
	})
	dev := newFakeUploadDevice("dev-863-dry")
	dev.scripts[1] = "watchdog.js"
	dev.scripts[2] = "pool-pump.js"
	dev.scripts[3] = "heater.js"
	dev.scripts[4] = "myhome-link.js" // not carried by the binary
	dev.nextID = 4
	dev.kv["script/heater.js"] = ScriptVersion([]byte("// heater v1\n"))

	got := mustUpdate(t, dev, UpdateOptions{LocalDir: dir, DryRun: true})

	want := map[string]Outcome{
		"watchdog.js":    OutcomeWouldUpdate,
		"pool-pump.js":   OutcomeRefused, // guarded, no --include-pump
		"heater.js":      OutcomeUpToDate,
		"myhome-link.js": OutcomeNotPackaged,
	}
	for name, w := range want {
		if got[name].Outcome != w {
			t.Errorf("%s = %s (%s), want %s", name, got[name].Outcome, got[name].Reason, w)
		}
	}
	for _, m := range []string{"Script.Stop", "Script.PutCode", "Script.Create", "Script.SetConfig", "Script.Start", "KVS.Set", "KVS.Delete"} {
		if n := dev.calls[m]; n != 0 {
			t.Errorf("--dry-run called %s %d time(s)", m, n)
		}
	}
}

// A script the binary does not carry (the 2026-11-21 second run failed on
// myhome-link.js) is not a failure of the push.
func TestUpdateDeviceScripts_NotPackagedIsNotAFailure(t *testing.T) {
	withFastRetry(t, 1)
	dev := newFakeUploadDevice("dev-863-notpkg")
	dev.scripts[1] = "myhome-link.js"
	r, err := UpdateDeviceScripts(context.Background(), testr.New(t), types.ChannelDefault, dev, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s := outcomes(r)["myhome-link.js"]; s.Outcome != OutcomeNotPackaged {
		t.Errorf("outcome = %s, want not-packaged", s.Outcome)
	}
	if len(r.Failed()) != 0 {
		t.Errorf("a script the binary does not carry must not count as failed")
	}
}

func TestUpdateDeviceScripts_TargetLimitsToOneScript(t *testing.T) {
	withFastRetry(t, 1)
	dir := localScripts(t, map[string]string{"watchdog.js": "// w\n", "heater.js": "// h\n"})
	dev := newFakeUploadDevice("dev-863-target")
	dev.scripts[1] = "watchdog.js"
	dev.scripts[2] = "heater.js"
	dev.nextID = 2
	got := mustUpdate(t, dev, UpdateOptions{LocalDir: dir, Target: "heater.js"})
	if len(got) != 1 || got["heater.js"].Outcome != OutcomeUpdated {
		t.Errorf("got %+v, want only heater.js updated", got)
	}
}

// --- the pump guard ---

const (
	pumpOld = "// pump as it runs on the device, v1\n"
	pumpNew = "// pump v2\n"
)

func newPump(t *testing.T, id string) (*fakeUploadDevice, string) {
	t.Helper()
	dev := newFakeUploadDevice(id)
	dev.scripts[1] = "pool-pump.js"
	dev.scripts[2] = "watchdog.js"
	dev.nextID = 2
	dev.code[1] = pumpOld
	dev.kv["script/pool-pump.js"] = ScriptVersion([]byte(pumpOld))
	dev.kv["script/watchdog.js"] = ScriptVersion([]byte("// w\n"))
	return dev, localScripts(t, map[string]string{"pool-pump.js": pumpNew, "watchdog.js": "// w\n"})
}

func noCodeSent(t *testing.T, dev *fakeUploadDevice) {
	t.Helper()
	for _, m := range []string{"Script.Stop", "Script.PutCode", "KVS.Set"} {
		if n := dev.calls[m]; n != 0 {
			t.Errorf("%s called %d time(s); the pump must not be touched", m, n)
		}
	}
	if dev.code[1] != pumpOld {
		t.Errorf("pump code changed to %q", dev.code[1])
	}
}

func TestUpdateDeviceScripts_PumpRefusedByDefault(t *testing.T) {
	withFastRetry(t, 1)
	dev, dir := newPump(t, "dev-863-pump-default")
	got := mustUpdate(t, dev, UpdateOptions{LocalDir: dir})
	s := got["pool-pump.js"]
	if s.Outcome != OutcomeRefused || !strings.Contains(s.Reason, "--include-pump") {
		t.Errorf("pool-pump.js = %s (%q), want refused naming --include-pump", s.Outcome, s.Reason)
	}
	if got["watchdog.js"].Outcome != OutcomeUpToDate {
		t.Errorf("watchdog.js = %s, want up-to-date (the guard must not block other scripts)", got["watchdog.js"].Outcome)
	}
	noCodeSent(t, dev)
	// A refused script is not started either (it would restart a stopped pump).
	if dev.startCalls != 1 { // watchdog.js only
		t.Errorf("Script.Start called %d time(s), want 1 (watchdog.js only)", dev.startCalls)
	}
}

// An up-to-date pump script is not even started without --include-pump: a
// Script.Start on a stopped pump script is a change to the pump.
func TestUpdateDeviceScripts_UpToDatePumpIsNotStarted(t *testing.T) {
	withFastRetry(t, 1)
	dev := newFakeUploadDevice("dev-863-pump-uptodate")
	dev.scripts[1] = "pool-pump.js"
	dev.nextID = 1
	dev.code[1] = pumpNew
	dev.kv["script/pool-pump.js"] = ScriptVersion([]byte(pumpNew))
	dir := localScripts(t, map[string]string{"pool-pump.js": pumpNew})

	s := mustUpdate(t, dev, UpdateOptions{LocalDir: dir})["pool-pump.js"]
	if s.Outcome != OutcomeUpToDate {
		t.Errorf("= %s, want up-to-date", s.Outcome)
	}
	if dev.calls["Script.Start"] != 0 {
		t.Errorf("Script.Start called %d time(s) on the pump without --include-pump", dev.calls["Script.Start"])
	}
}

func TestUpdateDeviceScripts_PumpRefusedWhileARelayIsOn(t *testing.T) {
	withFastRetry(t, 1)
	dev, dir := newPump(t, "dev-863-pump-relay-on")
	dev.relays = []bool{false, true, false} // a Pro3 with one speed on
	s := mustUpdate(t, dev, UpdateOptions{LocalDir: dir, IncludeGuarded: true})["pool-pump.js"]
	if s.Outcome != OutcomeRefused || !strings.Contains(s.Reason, "switch:1 is ON") {
		t.Errorf("= %s (%q), want refused naming switch:1", s.Outcome, s.Reason)
	}
	noCodeSent(t, dev)
}

func TestUpdateDeviceScripts_PumpRefusedWhenRelayStateUnreadable(t *testing.T) {
	withFastRetry(t, 1)
	inner, dir := newPump(t, "dev-863-pump-relay-unreadable")
	dev := &failingCallDevice{fakeUploadDevice: inner, failMethod: "Switch.GetStatus"}
	s := mustUpdate(t, dev, UpdateOptions{LocalDir: dir, IncludeGuarded: true})["pool-pump.js"]
	if s.Outcome != OutcomeRefused || !strings.Contains(s.Reason, "cannot read the state of switch:0") {
		t.Errorf("= %s (%q), want refused: relay state unreadable", s.Outcome, s.Reason)
	}
	noCodeSent(t, inner)
}

func TestUpdateDeviceScripts_PumpUpdatedWithBackup(t *testing.T) {
	withFastRetry(t, 1)
	withFastSettle(t)
	dev, dir := newPump(t, "dev-863-pump-ok")
	backups := t.TempDir()
	s := mustUpdate(t, dev, UpdateOptions{LocalDir: dir, IncludeGuarded: true, BackupDir: backups})["pool-pump.js"]
	if s.Outcome != OutcomeUpdated {
		t.Fatalf("= %s (%q), want updated", s.Outcome, s.Reason)
	}
	if dev.code[1] != pumpNew {
		t.Errorf("device code = %q, want the new version", dev.code[1])
	}
	saved, err := os.ReadFile(s.Backup)
	if err != nil {
		t.Fatalf("backup %q: %v", s.Backup, err)
	}
	if string(saved) != pumpOld {
		t.Errorf("backup = %q, want the code that was replaced %q", saved, pumpOld)
	}
	if filepath.Dir(s.Backup) != backups {
		t.Errorf("backup written to %s, want under %s", s.Backup, backups)
	}
}

// The 2026-11-21 incident: the new pump script started and then stopped with
// out_of_memory. The guard must put the previous code back.
func TestUpdateDeviceScripts_PumpRolledBackOnOutOfMemory(t *testing.T) {
	withFastRetry(t, 1)
	withFastSettle(t)
	dev, dir := newPump(t, "dev-863-pump-oom")
	dev.statusAfterStart = func(id uint32, starts int) pkgscript.Status {
		if id == 1 && starts == 1 { // the new code's first start
			return pkgscript.Status{Running: false, Errors: []string{"out_of_memory"}}
		}
		return pkgscript.Status{Running: true}
	}
	s := mustUpdate(t, dev, UpdateOptions{LocalDir: dir, IncludeGuarded: true, BackupDir: t.TempDir()})["pool-pump.js"]
	if s.Outcome != OutcomeFailed || !s.RolledBack {
		t.Fatalf("= %s rolled_back=%v (%q), want failed and rolled back", s.Outcome, s.RolledBack, s.Reason)
	}
	if !strings.Contains(s.Reason, "out_of_memory") || !strings.Contains(s.Reason, "previous code restored and running") {
		t.Errorf("reason = %q, want the error and the restoration", s.Reason)
	}
	if dev.code[1] != pumpOld {
		t.Errorf("device code = %q, want the previous code back", dev.code[1])
	}
	if _, ok := dev.kv["script/pool-pump.js"]; ok {
		t.Errorf("marker still present after a rollback; it would claim the new version is installed")
	}
}

func TestUpdateDeviceScripts_PumpRolledBackOnBrokenTransfer(t *testing.T) {
	withFastRetry(t, 1)
	withFastSettle(t)
	dev, dir := newPump(t, "dev-863-pump-broken")
	dev.putCodeErr = func(call int) error {
		if call == 1 {
			return errors.New("timeout waiting for response")
		}
		return nil
	}
	s := mustUpdate(t, dev, UpdateOptions{LocalDir: dir, IncludeGuarded: true, BackupDir: t.TempDir()})["pool-pump.js"]
	if s.Outcome != OutcomeFailed || !s.RolledBack {
		t.Fatalf("= %s rolled_back=%v (%q), want failed and rolled back", s.Outcome, s.RolledBack, s.Reason)
	}
	if dev.code[1] != pumpOld {
		t.Errorf("device code = %q, want the previous code back", dev.code[1])
	}
}

func TestDownloadCode_MultiChunk(t *testing.T) {
	dev := newFakeUploadDevice("dev-863-getcode")
	want := strings.Repeat("abcdefghij", 5) + "xyz" // several getCodeChunk-sized reads
	dev.code[3] = want
	got, err := downloadCode(context.Background(), types.ChannelDefault, dev, 3)
	if err != nil || got != want {
		t.Errorf("downloadCode = %q, %v; want %q", got, err, want)
	}
}

// ctxCheckingDevice fails every call whose context is already done, like the
// real MQTT channel does.
type ctxCheckingDevice struct{ *fakeUploadDevice }

func (d *ctxCheckingDevice) CallE(ctx context.Context, via types.Channel, method string, params any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return d.fakeUploadDevice.CallE(ctx, via, method, params)
}

// An expired fleet-wide deadline (`ctl --wait 180s ... update '*'`) must not
// cut an upload between Script.Stop and the marker write: that leaves a
// stopped script with partial code, or a stale marker that makes the next
// push upload again (#863).
func TestExecuteUpload_CriticalSectionOutlivesCallerDeadline(t *testing.T) {
	withFastRetry(t, 1)
	inner := newFakeUploadDevice("dev-863-deadline")
	inner.scripts[1] = "watchdog.js"
	inner.nextID = 1
	dev := &ctxCheckingDevice{inner}
	code := []byte(strings.Repeat("// line\n", 600)) // > one 2048-byte chunk

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the caller's deadline has already expired
	plan := UploadPlan{Upload: true, Reason: "test", Key: "script/watchdog.js", NewVersion: ScriptVersion(code), marker: markerRead}

	out, err := ExecuteUpload(ctx, testr.New(t), types.ChannelDefault, dev, "watchdog.js", code, false, plan)
	if err != nil {
		t.Fatalf("upload cut by the caller's deadline: %v", err)
	}
	if inner.code[1] != string(code) {
		t.Errorf("device has %d bytes of code, want all %d", len(inner.code[1]), len(code))
	}
	if inner.kv["script/watchdog.js"] != ScriptVersion(code) {
		t.Errorf("marker not recorded")
	}
	// The start is outside the critical section and does honour the deadline.
	if out.Status != StatusIndeterminate {
		t.Errorf("status = %s, want indeterminate (start not attempted past the deadline)", out.Status)
	}
}
