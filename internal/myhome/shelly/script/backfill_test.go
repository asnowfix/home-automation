package script

import (
	"context"
	"errors"
	"strings"
	"testing"

	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr/testr"
)

// backfillSrc is the "packaged" source used by the tests: long enough that the
// 7-byte fake GetCode chunks exercise the multi-chunk path, with a comment so
// its minified form differs from the source.
const backfillSrc = "// backfill fixture\nfunction hello(name) {\n  /* greet */\n  return \"hello \" + name;\n}\n"

func mustPrepare(t *testing.T, src string, minify bool) string {
	t.Helper()
	out, err := pkgscript.PrepareCode([]byte(src), minify)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// backfillFixture returns a device with watchdog.js loaded, holding installed,
// and the local dir that provides src as the packaged version.
func backfillFixture(t *testing.T, id, installed string) (*fakeUploadDevice, string) {
	t.Helper()
	dev := newFakeUploadDevice(id)
	dev.rejectDots = true // Gen3 firmware: only the dotless key works
	dev.scripts[1] = "watchdog.js"
	dev.nextID = 1
	dev.code[1] = installed
	return dev, localScripts(t, map[string]string{"watchdog.js": backfillSrc})
}

func mustBackfill(t *testing.T, dev types.Device, opts BackfillOptions) map[string]ScriptUpdate {
	t.Helper()
	r, err := BackfillDeviceMarkers(context.Background(), testr.New(t), types.ChannelDefault, dev, opts)
	if err != nil {
		t.Fatalf("BackfillDeviceMarkers: %v", err)
	}
	return outcomes(r)
}

func wantOutcome(t *testing.T, got map[string]ScriptUpdate, name string, want Outcome) ScriptUpdate {
	t.Helper()
	if got[name].Outcome != want {
		t.Fatalf("%s = %q (%s), want %q", name, got[name].Outcome, got[name].Reason, want)
	}
	return got[name]
}

// The acceptance criterion of #897, in process: after the backfill of a script
// installed in its minified form, `script update` reports it up-to-date and
// sends no code.
func TestBackfill_MinifiedMatchIsMarkedAndThenUpToDate(t *testing.T) {
	withFastRetry(t, 1)
	minified := mustPrepare(t, backfillSrc, true)
	if minified == backfillSrc {
		t.Fatal("fixture must differ when minified")
	}
	dev, dir := backfillFixture(t, "dev-897-min", minified)

	got := mustBackfill(t, dev, BackfillOptions{LocalDir: dir})
	su := wantOutcome(t, got, "watchdog.js", OutcomeMarked)
	if su.OldVersion != "none" || su.NewVersion != ScriptVersion([]byte(backfillSrc)) {
		t.Errorf("versions = %s -> %s", su.OldVersion, su.NewVersion)
	}
	if v := dev.kv["script/watchdog"]; v != ScriptVersion([]byte(backfillSrc)) {
		t.Errorf("marker at the dotless key = %q, want the sha1 of the packaged source, not of the installed text", v)
	}
	if len(dev.kv) != 1 {
		t.Errorf("kv = %v, want only the dotless key", dev.kv)
	}

	resetCalls(dev)
	upd := mustUpdate(t, dev, UpdateOptions{LocalDir: dir, DryRun: true})
	wantOutcome(t, upd, "watchdog.js", OutcomeUpToDate)
	for _, m := range []string{"Script.Stop", "Script.PutCode", "KVS.Set"} {
		if dev.calls[m] != 0 {
			t.Errorf("%s called %d time(s) after the backfill", m, dev.calls[m])
		}
	}
	if dev.code[1] != minified {
		t.Error("the installed code was touched")
	}
}

func TestBackfill_UnminifiedMatchIsMarked(t *testing.T) {
	dev, dir := backfillFixture(t, "dev-897-raw", backfillSrc)
	wantOutcome(t, mustBackfill(t, dev, BackfillOptions{LocalDir: dir}), "watchdog.js", OutcomeMarked)
	if dev.kv["script/watchdog"] != ScriptVersion([]byte(backfillSrc)) {
		t.Errorf("kv = %v", dev.kv)
	}
}

func TestBackfill_DifferingCodeIsLeftAlone(t *testing.T) {
	dev, dir := backfillFixture(t, "dev-897-diff", backfillSrc+"// edited on the device\n")
	wantOutcome(t, mustBackfill(t, dev, BackfillOptions{LocalDir: dir}), "watchdog.js", OutcomeDiffers)
	if dev.calls["KVS.Set"] != 0 || len(dev.kv) != 0 {
		t.Errorf("a differing script must not be marked: sets=%d kv=%v", dev.calls["KVS.Set"], dev.kv)
	}
}

func TestBackfill_ExistingMarkerIsLeftAlone(t *testing.T) {
	for _, key := range []string{"script/watchdog", "script/watchdog.js"} {
		t.Run(key, func(t *testing.T) {
			dev, dir := backfillFixture(t, "dev-897-have", backfillSrc)
			dev.rejectDots = false // the legacy key must be readable for this case
			dev.kv[key] = "0123456789012345678901234567890123456789"
			got := mustBackfill(t, dev, BackfillOptions{LocalDir: dir})
			su := wantOutcome(t, got, "watchdog.js", OutcomeAlreadyMarked)
			if su.OldVersion != dev.kv[key] {
				t.Errorf("old version = %q", su.OldVersion)
			}
			if dev.calls["KVS.Set"] != 0 || dev.calls["Script.GetCode"] != 0 {
				t.Errorf("an existing marker needs no write and no code read: %v", dev.calls)
			}
			if dev.kv[key] != "0123456789012345678901234567890123456789" || len(dev.kv) != 1 {
				t.Errorf("kv changed: %v", dev.kv)
			}
		})
	}
}

func TestBackfill_UnknownMarkerIsSkipped(t *testing.T) {
	dev, dir := backfillFixture(t, "dev-897-unk", backfillSrc)
	dev.kvGetErr = errors.New("timeout")
	wantOutcome(t, mustBackfill(t, dev, BackfillOptions{LocalDir: dir}), "watchdog.js", OutcomeSkipped)
	if dev.calls["KVS.Set"] != 0 || dev.calls["Script.GetCode"] != 0 {
		t.Errorf("no code read and no write when the marker is unreadable: %v", dev.calls)
	}
}

func TestBackfill_PumpIsNotTouchedAtAll(t *testing.T) {
	dev := newFakeUploadDevice("dev-897-pump")
	dev.scripts[1] = "pool-pump.js"
	dev.nextID = 1
	dev.code[1] = pumpOld
	dir := localScripts(t, map[string]string{"pool-pump.js": pumpOld})

	wantOutcome(t, mustBackfill(t, dev, BackfillOptions{LocalDir: dir}), "pool-pump.js", OutcomeSkipped)
	for m, n := range dev.calls {
		if m != "Script.List" && n != 0 {
			t.Errorf("pool-pump.js caused %s x%d; only the list may be read", m, n)
		}
	}
}

func TestBackfill_NotPackagedIsSkipped(t *testing.T) {
	dev, dir := backfillFixture(t, "dev-897-nopkg", backfillSrc)
	dev.scripts[2] = "hand-written.js"
	got := mustBackfill(t, dev, BackfillOptions{LocalDir: dir})
	wantOutcome(t, got, "hand-written.js", OutcomeSkipped)
	wantOutcome(t, got, "watchdog.js", OutcomeMarked)
}

func TestBackfill_DryRunWritesNothing(t *testing.T) {
	dev, dir := backfillFixture(t, "dev-897-dry", mustPrepare(t, backfillSrc, true))
	wantOutcome(t, mustBackfill(t, dev, BackfillOptions{LocalDir: dir, DryRun: true}), "watchdog.js", OutcomeWouldMark)
	allowed := map[string]bool{"Script.List": true, "KVS.Get": true, "Script.GetCode": true}
	for m, n := range dev.calls {
		if n != 0 && !allowed[m] {
			t.Errorf("dry run issued %s x%d", m, n)
		}
	}
	if len(dev.kv) != 0 {
		t.Errorf("kv = %v", dev.kv)
	}
}

func TestBackfill_GetCodeErrorIsSkipped(t *testing.T) {
	base, dir := backfillFixture(t, "dev-897-getcode", backfillSrc)
	dev := &getCodeFailingDevice{fakeUploadDevice: base}
	wantOutcome(t, mustBackfill(t, dev, BackfillOptions{LocalDir: dir}), "watchdog.js", OutcomeSkipped)
	if base.calls["KVS.Set"] != 0 {
		t.Error("no write when the code could not be read")
	}
}

type getCodeFailingDevice struct{ *fakeUploadDevice }

func (d *getCodeFailingDevice) CallE(ctx context.Context, via types.Channel, method string, params any) (any, error) {
	if method == "Script.GetCode" {
		return nil, errors.New("timeout")
	}
	return d.fakeUploadDevice.CallE(ctx, via, method, params)
}

func TestBackfill_MarkerWriteFailureIsFailed(t *testing.T) {
	dev, dir := backfillFixture(t, "dev-897-setfail", backfillSrc)
	dev.kvSetErr = errors.New("flash full")
	su := wantOutcome(t, mustBackfill(t, dev, BackfillOptions{LocalDir: dir}), "watchdog.js", OutcomeFailed)
	if !strings.Contains(su.Reason, "flash full") {
		t.Errorf("reason = %q", su.Reason)
	}
}

func TestBackfill_TargetLimitsToOneScript(t *testing.T) {
	dev, dir := backfillFixture(t, "dev-897-target", backfillSrc)
	dev.scripts[2] = "other.js"
	got := mustBackfill(t, dev, BackfillOptions{LocalDir: dir, Target: "other.js"})
	if len(got) != 1 {
		t.Errorf("got %v, want only other.js", got)
	}
	if dev.calls["KVS.Set"] != 0 {
		t.Error("watchdog.js must not be touched")
	}
}
