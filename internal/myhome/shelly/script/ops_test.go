package script

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr/testr"
)

// TestShouldUpload covers the decision that #449 got wrong: a version marker
// matching the code is not proof the device still has the script.
func TestShouldUpload(t *testing.T) {
	const version = "abc123"

	tests := []struct {
		name       string
		force      bool
		want       string
		kvsVersion string
		present    bool
		wantUpload bool
	}{
		{
			name:       "version differs, script present",
			want:       version,
			kvsVersion: "old",
			present:    true,
			wantUpload: true,
		},
		{
			name:       "version matches and script is present: skip",
			want:       version,
			kvsVersion: version,
			present:    true,
			wantUpload: false,
		},
		{
			// The #449 case. Before the fix this skipped the upload, then tried
			// to start a script the device does not have — reporting either
			// "script not found" or, when something had recreated it empty,
			// success for a script that does nothing.
			name:       "version matches but script is gone: upload anyway",
			want:       version,
			kvsVersion: version,
			present:    false,
			wantUpload: true,
		},
		{
			name:       "force overrides a matching version",
			force:      true,
			want:       version,
			kvsVersion: version,
			present:    true,
			wantUpload: true,
		},
		{
			name:       "no marker recorded yet",
			want:       version,
			kvsVersion: "",
			present:    false,
			wantUpload: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := shouldUpload(tc.force, tc.want, tc.kvsVersion, tc.present)
			if got != tc.wantUpload {
				t.Errorf("shouldUpload(force=%v, want=%q, kvs=%q, present=%v) = %v (%q), want %v",
					tc.force, tc.want, tc.kvsVersion, tc.present, got, reason, tc.wantUpload)
			}
			if reason == "" {
				t.Error("expected a non-empty reason for the log line")
			}
		})
	}
}

// errTransport simulates a non-timeout transport error (e.g. connection
// reset) returned by a follow-up RPC, distinct from context.DeadlineExceeded.
var errTransport = errors.New("connection reset by peer")

// TestClassifyUpload covers issue #428: a timeout or transport error on the
// post-upload confirmation step (enabling/starting the script) must not be
// reported as an upload failure once the chunked code transfer itself
// succeeded. This is the pure decision function extracted so it is directly
// testable without a device or a live MQTT broker.
func TestClassifyUpload(t *testing.T) {
	tests := []struct {
		name       string
		chunkErr   error
		confirmErr error
		wantStatus UploadStatus
	}{
		{
			name:       "upload fails outright",
			chunkErr:   errors.New("PutCode chunk 2/5: no response"),
			confirmErr: nil, // never attempted; chunk transfer itself failed
			wantStatus: StatusFailed,
		},
		{
			name:       "upload succeeds and verification succeeds",
			chunkErr:   nil,
			confirmErr: nil,
			wantStatus: StatusUploaded,
		},
		{
			name:       "upload succeeds and verification times out",
			chunkErr:   nil,
			confirmErr: context.DeadlineExceeded,
			wantStatus: StatusIndeterminate,
		},
		{
			name:       "verification returns a transport error that is not a timeout",
			chunkErr:   nil,
			confirmErr: errTransport,
			wantStatus: StatusIndeterminate,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, message := classifyUpload(tc.chunkErr, tc.confirmErr)
			if status != tc.wantStatus {
				t.Errorf("classifyUpload(chunkErr=%v, confirmErr=%v) = %v (%q), want %v",
					tc.chunkErr, tc.confirmErr, status, message, tc.wantStatus)
			}
			if message == "" {
				t.Error("expected a non-empty message for the operator")
			}
			// A genuine failure must never carry a "confirm your status,
			// don't re-upload" message, and vice versa — the message and
			// the exit-code-driving status must agree.
			if status == StatusFailed && tc.chunkErr == nil {
				t.Error("StatusFailed returned without a chunk error")
			}
			if status == StatusIndeterminate && tc.chunkErr != nil {
				t.Error("StatusIndeterminate returned despite a chunk error")
			}
		})
	}
}

// TestRetryBounded proves the retry helper used to confirm a script's start
// is genuinely bounded (issue #428's "consider a bounded retry" direction)
// and that it stops retrying at the first success.
func TestRetryBounded(t *testing.T) {
	t.Run("succeeds on first attempt: no wait, no retry", func(t *testing.T) {
		calls := 0
		err := retryBounded(context.Background(), 3, time.Hour, func(attempt int) error {
			calls++
			return nil
		})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if calls != 1 {
			t.Errorf("expected exactly 1 call, got %d", calls)
		}
	})

	t.Run("succeeds on a later attempt within the bound", func(t *testing.T) {
		calls := 0
		err := retryBounded(context.Background(), 3, time.Millisecond, func(attempt int) error {
			calls++
			if attempt < 3 {
				return errTransport
			}
			return nil
		})
		if err != nil {
			t.Fatalf("expected nil error after eventual success, got %v", err)
		}
		if calls != 3 {
			t.Errorf("expected exactly 3 calls, got %d", calls)
		}
	})

	t.Run("gives up after exhausting attempts and returns the last error", func(t *testing.T) {
		calls := 0
		wantErr := errors.New("attempt failure")
		err := retryBounded(context.Background(), 3, time.Millisecond, func(attempt int) error {
			calls++
			return wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
		if calls != 3 {
			t.Errorf("expected exactly 3 calls (bounded), got %d", calls)
		}
	})

	t.Run("stops early on context cancellation between attempts", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		err := retryBounded(ctx, 5, 50*time.Millisecond, func(attempt int) error {
			calls++
			if attempt == 1 {
				cancel()
			}
			return errTransport
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if calls != 1 {
			t.Errorf("expected exactly 1 call before cancellation was observed, got %d", calls)
		}
	})
}

// ---------------------------------------------------------------------------
// fakeUploadDevice — a minimal types.Device double that exercises
// UploadWithVersionDetailed end-to-end (chunked upload, KVS version
// tracking, and the post-upload Script.Start confirmation) without a real
// device or MQTT broker. Modeled on pkg/shelly/script's fakeDevice.
// ---------------------------------------------------------------------------

type fakeUploadDevice struct {
	id      string
	scripts map[uint32]string // id -> name: scripts the device currently has
	nextID  uint32
	kv      map[string]string

	// startErr, if set, is consulted on every Script.Start attempt (1-based);
	// a non-nil return simulates the confirmation step failing (timeout,
	// transport error, ...).
	startErr   func(attempt int) error
	startCalls int
}

var _ types.Device = (*fakeUploadDevice)(nil)

func newFakeUploadDevice(id string) *fakeUploadDevice {
	return &fakeUploadDevice{id: id, scripts: map[uint32]string{}, kv: map[string]string{}}
}

func (f *fakeUploadDevice) CallE(_ context.Context, _ types.Channel, method string, params any) (any, error) {
	switch method {
	case "Script.List":
		list := make([]pkgscript.Status, 0, len(f.scripts))
		for id, name := range f.scripts {
			list = append(list, pkgscript.Status{Id: id, Name: name})
		}
		return &pkgscript.ListResponse{Scripts: list}, nil

	case "Script.Create":
		req := params.(*pkgscript.Configuration)
		f.nextID++
		f.scripts[f.nextID] = req.Name
		return &pkgscript.Status{Id: f.nextID, Name: req.Name}, nil

	case "Script.Stop":
		return &pkgscript.Status{}, nil

	case "Script.PutCode":
		return &pkgscript.PutCodeResponse{}, nil

	case "Script.SetConfig":
		return &pkgscript.Status{}, nil

	case "Script.Start":
		f.startCalls++
		if f.startErr != nil {
			if err := f.startErr(f.startCalls); err != nil {
				return nil, err
			}
		}
		return &pkgscript.Status{}, nil

	case "KVS.Get":
		req := params.(*kvs.GetRequest)
		v, ok := f.kv[req.Key]
		if !ok {
			return nil, fmt.Errorf("fakeUploadDevice: key %q not found", req.Key)
		}
		return &kvs.GetResponse{Value: v}, nil

	case "KVS.Set":
		req := params.(*kvs.KeyValue)
		f.kv[req.Key] = req.Value
		return &kvs.Status{}, nil

	default:
		return nil, fmt.Errorf("fakeUploadDevice: unexpected method %q", method)
	}
}

func (f *fakeUploadDevice) String() string                                             { return "fake-upload-device" }
func (f *fakeUploadDevice) Name() string                                               { return "fake-upload-device" }
func (f *fakeUploadDevice) Host() string                                               { return "fake" }
func (f *fakeUploadDevice) Manufacturer() string                                       { return "fake" }
func (f *fakeUploadDevice) Id() string                                                 { return f.id }
func (f *fakeUploadDevice) Mac() net.HardwareAddr                                      { return nil }
func (f *fakeUploadDevice) ReplyTo() string                                            { return "" }
func (f *fakeUploadDevice) To() chan<- []byte                                          { return nil }
func (f *fakeUploadDevice) From() <-chan []byte                                        { return nil }
func (f *fakeUploadDevice) StartDialog(_ context.Context) uint32                       { return 0 }
func (f *fakeUploadDevice) StopDialog(_ context.Context, _ uint32)                     {}
func (f *fakeUploadDevice) IsHttpReady() bool                                          { return false }
func (f *fakeUploadDevice) IsMqttReady() bool                                          { return true }
func (f *fakeUploadDevice) Channel(_ context.Context, via types.Channel) types.Channel { return via }
func (f *fakeUploadDevice) UpdateName(_ string)                                        {}
func (f *fakeUploadDevice) UpdateHost(_ string)                                        {}
func (f *fakeUploadDevice) ClearHost()                                                 {}
func (f *fakeUploadDevice) UpdateMac(_ string)                                         {}
func (f *fakeUploadDevice) UpdateId(_ string)                                          {}
func (f *fakeUploadDevice) IsModified() bool                                           { return false }
func (f *fakeUploadDevice) ResetModified()                                             {}

// withFastRetry shrinks startAttempts/startRetryDelay for the duration of a
// test, restoring the production values in t.Cleanup (the package-level
// vars are shared, untested code must never observe the shrunk values).
func withFastRetry(t *testing.T, attempts int) {
	t.Helper()
	origAttempts, origDelay := startAttempts, startRetryDelay
	startAttempts = attempts
	startRetryDelay = time.Millisecond
	t.Cleanup(func() {
		startAttempts = origAttempts
		startRetryDelay = origDelay
	})
}

// TestUploadWithVersionDetailed_StartTimeoutIsIndeterminate is the
// regression test for issue #428: every PutCode chunk was acknowledged
// (confirmed by the fake device's Script.List reflecting the new script
// afterwards) but Script.Start never confirms. Before the fix, this made
// UploadWithVersion return a non-nil error and the CLI report "upload
// failed" even though the code was on the device the whole time.
func TestUploadWithVersionDetailed_StartTimeoutIsIndeterminate(t *testing.T) {
	withFastRetry(t, 3)
	log := testr.New(t)
	dev := newFakeUploadDevice("device-428-timeout")
	dev.startErr = func(attempt int) error { return context.DeadlineExceeded }

	id, status, err := UploadWithVersionDetailed(context.Background(), log, types.ChannelDefault, dev, "pool-pump.js", []byte("// v1\n"), false, false)
	if err != nil {
		t.Fatalf("UploadWithVersionDetailed returned an error for a confirmed upload with only a Start timeout: %v", err)
	}
	if status != StatusIndeterminate {
		t.Errorf("status = %v, want %v", status, StatusIndeterminate)
	}
	if id == 0 {
		t.Error("expected a non-zero script id: the code transfer succeeded")
	}
	if dev.startCalls != 3 {
		t.Errorf("expected the bounded retry to make exactly 3 Script.Start attempts, got %d", dev.startCalls)
	}

	// The literal bug report: UploadWithVersion (the two-value contract most
	// callers use) must not turn this into an error either.
	dev2 := newFakeUploadDevice("device-428-timeout-2")
	dev2.startErr = func(attempt int) error { return context.DeadlineExceeded }
	if _, err := UploadWithVersion(context.Background(), log, types.ChannelDefault, dev2, "pool-pump.js", []byte("// v1\n"), false, false); err != nil {
		t.Fatalf("UploadWithVersion reported failure for a script that was actually uploaded: %v (this is exactly issue #428)", err)
	}
}

// TestUploadWithVersionDetailed_StartSucceedsAfterRetry proves the bounded
// retry itself does useful work: a Start that only confirms on the 2nd
// attempt is reported as a fully confirmed upload, not indeterminate.
func TestUploadWithVersionDetailed_StartSucceedsAfterRetry(t *testing.T) {
	withFastRetry(t, 3)
	log := testr.New(t)
	dev := newFakeUploadDevice("device-428-retry-recovers")
	dev.startErr = func(attempt int) error {
		if attempt < 2 {
			return context.DeadlineExceeded
		}
		return nil
	}

	id, status, err := UploadWithVersionDetailed(context.Background(), log, types.ChannelDefault, dev, "pool-pump.js", []byte("// v1\n"), false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != StatusUploaded {
		t.Errorf("status = %v, want %v (retry should have recovered)", status, StatusUploaded)
	}
	if id == 0 {
		t.Error("expected a non-zero script id")
	}
}

// TestUploadWithVersionDetailed_AllConfirmed is the control case: nothing
// times out, everything is confirmed.
func TestUploadWithVersionDetailed_AllConfirmed(t *testing.T) {
	log := testr.New(t)
	dev := newFakeUploadDevice("device-428-happy-path")

	id, status, err := UploadWithVersionDetailed(context.Background(), log, types.ChannelDefault, dev, "pool-pump.js", []byte("// v1\n"), false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != StatusUploaded {
		t.Errorf("status = %v, want %v", status, StatusUploaded)
	}
	if id == 0 {
		t.Error("expected a non-zero script id")
	}
	if dev.startCalls != 1 {
		t.Errorf("expected exactly 1 Script.Start call when it succeeds immediately, got %d", dev.startCalls)
	}
}

// TestUploadWithVersionDetailed_ChunkFailureIsGenuineFailure proves the
// other half of the classification: when the code transfer itself does not
// complete, this remains a real failure (non-nil error, StatusFailed) —
// the fix must not swallow genuine failures.
func TestUploadWithVersionDetailed_ChunkFailureIsGenuineFailure(t *testing.T) {
	log := testr.New(t)
	dev := newFakeUploadDevice("device-428-chunk-fails")
	// Fail at the RPC dispatch layer itself: PutCode always errors, so no
	// chunk is ever acknowledged.
	dev.scripts = map[uint32]string{} // starts empty, forcing Script.Create

	failing := &failingCallDevice{fakeUploadDevice: dev, failMethod: "Script.PutCode"}

	id, status, err := UploadWithVersionDetailed(context.Background(), log, types.ChannelDefault, failing, "pool-pump.js", []byte("// v1\n"), false, false)
	if err == nil {
		t.Fatal("expected an error: the chunk transfer never completed")
	}
	if status != StatusFailed {
		t.Errorf("status = %v, want %v", status, StatusFailed)
	}
	if id != 0 {
		t.Errorf("id = %d, want 0 for a genuine failure", id)
	}
}

// failingCallDevice wraps fakeUploadDevice and forces one RPC method to
// always fail, regardless of the fake's own state — used to simulate a
// chunk transfer that never completes.
type failingCallDevice struct {
	*fakeUploadDevice
	failMethod string
}

func (f *failingCallDevice) CallE(ctx context.Context, via types.Channel, method string, params any) (any, error) {
	if method == f.failMethod {
		return nil, errTransport
	}
	return f.fakeUploadDevice.CallE(ctx, via, method, params)
}
