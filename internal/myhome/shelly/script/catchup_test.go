// catchup_test.go — #897: the one-time version-marker catch-up. A script that
// is loaded on the device but has no marker is compared with the packaged
// source once; identical code gets its marker recorded instead of being
// uploaded and restarted. Runs against fakeUploadDevice (ops_test.go); no
// device is touched.
package script

import (
	"context"
	"testing"

	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr/testr"
)

// A source the minifier visibly changes (comments, whitespace), so the
// minified-equal case is not the raw-equal case in disguise.
const catchUpSrc = "// heater v1\nvar answer = 42; // the answer\nfunction add(x, y) {\n  return x + y;\n}\n"

func minifiedCatchUpSrc(t *testing.T) string {
	t.Helper()
	min, err := pkgscript.PrepareCode([]byte(catchUpSrc), true)
	if err != nil {
		t.Fatalf("PrepareCode: %v", err)
	}
	if string(min) == catchUpSrc {
		t.Fatal("test source is not changed by minification; pick another")
	}
	return string(min)
}

func TestUpdateDeviceScripts_MarkerCatchUp(t *testing.T) {
	withFastRetry(t, 1)
	version := ScriptVersion([]byte(catchUpSrc))
	minified := minifiedCatchUpSrc(t)

	tests := []struct {
		name      string
		script    string // loaded script name
		installed string // its code on the device
		marker    string // value of script/<name>; "" means missing
		getCodeKO bool   // Script.GetCode fails
		setReject bool   // KVS.Set of the marker is refused
		opts      UpdateOptions

		want        Outcome
		wantUpload  bool   // Script.PutCode was called
		wantMarker  string // expected marker after the run ("" = absent)
		wantGetCode bool   // Script.GetCode was called
		wantStart   bool   // Script.Start was called
	}{
		{
			name:   "missing + identical (raw): marker restored, no upload",
			script: "heater.js", installed: catchUpSrc,
			want: OutcomeMarkerRestored, wantMarker: version, wantGetCode: true, wantStart: true,
		},
		{
			name:   "missing + identical (minified): marker restored, no upload",
			script: "heater.js", installed: minified,
			want: OutcomeMarkerRestored, wantMarker: version, wantGetCode: true, wantStart: true,
		},
		{
			name:   "missing + different: uploaded as before",
			script: "heater.js", installed: "// heater v0\n",
			want: OutcomeUpdated, wantUpload: true, wantMarker: version, wantGetCode: true, wantStart: true,
		},
		{
			name:   "missing + GetCode error: skipped, nothing written or started",
			script: "heater.js", installed: catchUpSrc, getCodeKO: true,
			want: OutcomeSkipped, wantGetCode: true,
		},
		{
			name:   "missing + identical, marker write refused: failed, no upload",
			script: "heater.js", installed: catchUpSrc, setReject: true,
			want: OutcomeFailed, wantGetCode: true, wantStart: true,
		},
		{
			name:   "dry-run + identical: would-mark, read-only",
			script: "heater.js", installed: catchUpSrc, opts: UpdateOptions{DryRun: true},
			want: OutcomeWouldMark, wantGetCode: true,
		},
		{
			name:   "dry-run + different: would-update, read-only",
			script: "heater.js", installed: "// heater v0\n", opts: UpdateOptions{DryRun: true},
			want: OutcomeWouldUpdate, wantGetCode: true,
		},
		{
			name:   "dry-run + GetCode error: skipped",
			script: "heater.js", installed: catchUpSrc, getCodeKO: true, opts: UpdateOptions{DryRun: true},
			want: OutcomeSkipped, wantGetCode: true,
		},
		{
			name:   "force + identical: uploaded, code never read",
			script: "heater.js", installed: catchUpSrc, opts: UpdateOptions{Force: true},
			want: OutcomeUpdated, wantUpload: true, wantMarker: version, wantStart: true,
		},
		{
			name:   "steady state (marker present): one KVS.Get, no code read",
			script: "heater.js", installed: catchUpSrc, marker: version,
			want: OutcomeUpToDate, wantMarker: version, wantStart: true,
		},
		{
			name:   "pump + identical, no --include-pump: refused, never read, no marker",
			script: "pool-pump.js", installed: catchUpSrc,
			want: OutcomeRefused,
		},
		{
			name:   "pump + identical, dry-run: refused, never read",
			script: "pool-pump.js", installed: catchUpSrc, opts: UpdateOptions{DryRun: true},
			want: OutcomeRefused,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			opts.LocalDir = localScripts(t, map[string]string{tc.script: catchUpSrc})

			inner := newFakeUploadDevice("dev-897-catchup")
			inner.scripts[1] = tc.script
			inner.nextID = 1
			inner.code[1] = tc.installed
			key, _ := MarkerKeys(tc.script)
			if tc.marker != "" {
				inner.kv[key] = tc.marker
			}
			if tc.setReject {
				inner.kvSetReject = map[string]bool{key: true}
			}
			var dev types.Device = inner
			if tc.getCodeKO {
				dev = &countingFailDevice{failingCallDevice{fakeUploadDevice: inner, failMethod: "Script.GetCode"}}
			}

			got := mustUpdate(t, dev, opts)[tc.script]
			if got.Outcome != tc.want {
				t.Fatalf("outcome = %s (%q), want %s", got.Outcome, got.Reason, tc.want)
			}
			if up := inner.calls["Script.PutCode"] > 0; up != tc.wantUpload {
				t.Errorf("uploaded = %v (PutCode=%d), want %v", up, inner.calls["Script.PutCode"], tc.wantUpload)
			}
			if !tc.wantUpload && inner.calls["Script.Stop"] != 0 {
				t.Errorf("Script.Stop called %d time(s) without an upload", inner.calls["Script.Stop"])
			}
			if v := inner.kv[key]; v != tc.wantMarker {
				t.Errorf("marker %s = %q, want %q", key, v, tc.wantMarker)
			}
			if rd := inner.calls["Script.GetCode"] > 0; rd != tc.wantGetCode {
				t.Errorf("Script.GetCode called = %v, want %v", rd, tc.wantGetCode)
			}
			if st := inner.calls["Script.Start"] > 0; st != tc.wantStart {
				t.Errorf("Script.Start called = %v, want %v", st, tc.wantStart)
			}
			if opts.DryRun || tc.script == "pool-pump.js" {
				for _, m := range []string{"KVS.Set", "KVS.Delete", "Script.Start", "Script.Stop", "Script.PutCode"} {
					if n := inner.calls[m]; n != 0 {
						t.Errorf("%s called %d time(s), want none", m, n)
					}
				}
			}
			if got.Outcome == OutcomeMarkerRestored || got.Outcome == OutcomeWouldMark {
				if got.OldVersion != "none" || got.NewVersion != version {
					t.Errorf("versions = %q -> %q, want none -> %s", got.OldVersion, got.NewVersion, version)
				}
			}
			if r := (&DeviceScriptsReport{Scripts: []ScriptUpdate{got}}); (got.Outcome == OutcomeSkipped) && len(r.Failed()) != 0 {
				t.Error("skipped must not count as a failure")
			}
		})
	}
}

// countingFailDevice is failingCallDevice that still counts the failed call,
// so a test can tell "not attempted" from "attempted and failed".
type countingFailDevice struct{ failingCallDevice }

func (d *countingFailDevice) CallE(ctx context.Context, via types.Channel, method string, params any) (any, error) {
	if method == d.failMethod {
		d.calls[method]++
	}
	return d.failingCallDevice.CallE(ctx, via, method, params)
}

// After a catch-up the next run is plain steady state: up to date, no code read.
func TestUpdateDeviceScripts_MarkerCatchUpThenSteadyState(t *testing.T) {
	withFastRetry(t, 1)
	dir := localScripts(t, map[string]string{"heater.js": catchUpSrc})
	dev := newFakeUploadDevice("dev-897-then")
	dev.scripts[1] = "heater.js"
	dev.nextID = 1
	dev.code[1] = catchUpSrc

	if got := mustUpdate(t, dev, UpdateOptions{LocalDir: dir})["heater.js"]; got.Outcome != OutcomeMarkerRestored {
		t.Fatalf("first run = %s (%q)", got.Outcome, got.Reason)
	}
	resetCalls(dev)
	if got := mustUpdate(t, dev, UpdateOptions{LocalDir: dir})["heater.js"]; got.Outcome != OutcomeUpToDate {
		t.Fatalf("second run = %s (%q)", got.Outcome, got.Reason)
	}
	if n := dev.calls["Script.GetCode"]; n != 0 {
		t.Errorf("steady state read the code %d time(s)", n)
	}
}

// The `script upload` path (UploadWithVersionDetailed, presence looked up by
// PlanUpload itself) gets the same catch-up.
func TestUploadWithVersionDetailed_MarkerCatchUp(t *testing.T) {
	withFastRetry(t, 1)
	for _, tc := range []struct {
		name       string
		installed  string
		wantUpload bool
	}{
		{"identical", catchUpSrc, false},
		{"minified identical", minifiedCatchUpSrc(t), false},
		{"different", "// other\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dev := newFakeUploadDevice("dev-897-upload")
			dev.scripts[3] = "heater.js"
			dev.nextID = 3
			dev.code[3] = tc.installed

			_, _, err := UploadWithVersionDetailed(context.Background(), testr.New(t), types.ChannelDefault, dev, "heater.js", []byte(catchUpSrc), true, false)
			if err != nil {
				t.Fatal(err)
			}
			if up := dev.calls["Script.PutCode"] > 0; up != tc.wantUpload {
				t.Errorf("uploaded = %v, want %v", up, tc.wantUpload)
			}
			if got, want := dev.kv["script/heater"], ScriptVersion([]byte(catchUpSrc)); got != want {
				t.Errorf("marker = %q, want %q", got, want)
			}
		})
	}
}

// PlanUpload is read-only even when it reads the code.
func TestPlanUpload_MarkerCatchUpIsReadOnly(t *testing.T) {
	dev := newFakeUploadDevice("dev-897-plan")
	dev.scripts[1] = "heater.js"
	dev.code[1] = catchUpSrc
	plan := PlanUpload(context.Background(), testr.New(t), types.ChannelDefault, dev, "heater.js", []byte(catchUpSrc), false, nil)
	if plan.Upload || plan.CatchUp != CatchUpIdentical || plan.OldVersionLabel() != "none" {
		t.Fatalf("plan = %+v", plan)
	}
	for _, m := range []string{"KVS.Set", "KVS.Delete", "Script.Start", "Script.Stop", "Script.PutCode"} {
		if n := dev.calls[m]; n != 0 {
			t.Errorf("%s called %d time(s) while planning", m, n)
		}
	}
}

func TestMatchesPackaged(t *testing.T) {
	minified := minifiedCatchUpSrc(t)
	for _, tc := range []struct {
		name      string
		installed string
		src       string
		want      bool
	}{
		{"raw", catchUpSrc, catchUpSrc, true},
		{"minified", minified, catchUpSrc, true},
		{"different", "// other\n", catchUpSrc, false},
		{"trailing newline differs", catchUpSrc + "\n", catchUpSrc, false},
		// #449: an empty script must be uploaded, even when the packaged
		// source is comment-only and so minifies to nothing.
		{"empty installed, comment-only source", "", "// only a comment\n", false},
		{"empty installed", "", catchUpSrc, false},
	} {
		if got := matchesPackaged(tc.installed, []byte(tc.src)); got != tc.want {
			t.Errorf("%s: matchesPackaged = %v, want %v", tc.name, got, tc.want)
		}
	}
}
