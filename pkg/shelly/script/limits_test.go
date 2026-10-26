// limits_test.go — issue #553: the device's Script.PutCode limit is checked
// client-side, before any RPC is attempted, and refuses with a clear message
// naming the limit and the actual size.
package script

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/types"
)

// TestCheckSourceLength_TableDriven exercises the pure check directly: a
// source at or under MaxSourceBytes must pass through untouched (nil error);
// one over it must be rejected, with wording that differs depending on
// whether minification was requested, since that determines whether
// --no-minify is even a viable alternative.
func TestCheckSourceLength_TableDriven(t *testing.T) {
	tests := []struct {
		name            string
		size            int
		minifyRequested bool
		wantErr         bool
		wantContains    []string
	}{
		{
			name:            "exactly at the limit passes",
			size:            MaxSourceBytes,
			minifyRequested: false,
			wantErr:         false,
		},
		{
			name:            "well under the limit passes",
			size:            100,
			minifyRequested: true,
			wantErr:         false,
		},
		{
			name:            "one byte over, no-minify requested",
			size:            MaxSourceBytes + 1,
			minifyRequested: false,
			wantErr:         true,
			wantContains: []string{
				"65535",                  // the limit
				"65536",                  // the actual size
				"--no-minify cannot work",
			},
		},
		{
			name:            "over the limit even after minification",
			size:            MaxSourceBytes + 5000,
			minifyRequested: true,
			wantErr:         true,
			wantContains: []string{
				"65535",
				"70535",
				"after minification",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := bytes.Repeat([]byte("x"), tt.size)
			err := checkSourceLength("pool-pump.js", code, tt.minifyRequested)

			if tt.wantErr && err == nil {
				t.Fatalf("checkSourceLength() = nil, want an error for size %d", tt.size)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("checkSourceLength() = %v, want nil for size %d", err, tt.size)
			}
			if !tt.wantErr {
				return
			}

			var tooLarge *ScriptTooLargeError
			if !errors.As(err, &tooLarge) {
				t.Fatalf("checkSourceLength() error is %T, want *ScriptTooLargeError", err)
			}
			if tooLarge.Size != tt.size {
				t.Errorf("ScriptTooLargeError.Size = %d, want %d", tooLarge.Size, tt.size)
			}
			if tooLarge.Limit != MaxSourceBytes {
				t.Errorf("ScriptTooLargeError.Limit = %d, want %d", tooLarge.Limit, MaxSourceBytes)
			}

			msg := err.Error()
			for _, want := range tt.wantContains {
				if !strings.Contains(msg, want) {
					t.Errorf("error message %q does not contain %q", msg, want)
				}
			}
		})
	}
}

// TestUpload_RejectsOversizedSource_NoMinify reproduces the exact scenario
// from #553: pool-pump.js-sized source, uploaded with --no-minify (minify
// wired to false, as the CLI does), must be refused before any RPC is
// attempted — the device is never given the chance to reject it after the
// fact. This exercises the real Upload/doUpload entry point, not just the
// pure helper, so it also proves the check is reached from the code path a
// future caller (not only the `script upload` cobra command) would use.
func TestUpload_RejectsOversizedSource_NoMinify(t *testing.T) {
	dev := &fakeDevice{
		callFn: func(method string, params any) (any, error) {
			t.Fatalf("unexpected RPC call %s(%v): oversized source must be rejected before any RPC is attempted", method, params)
			return nil, nil
		},
	}

	oversized := bytes.Repeat([]byte("x"), MaxSourceBytes+1)

	_, err := Upload(context.Background(), types.ChannelDefault, dev, "pool-pump.js", oversized, false /* minify */)
	if err == nil {
		t.Fatal("Upload() = nil error, want a rejection for oversized source")
	}

	var tooLarge *ScriptTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("Upload() error is %T, want *ScriptTooLargeError: %v", err, err)
	}
	if tooLarge.MinifyRequested {
		t.Errorf("ScriptTooLargeError.MinifyRequested = true, want false (--no-minify was requested)")
	}
	if !strings.Contains(err.Error(), "--no-minify cannot work") {
		t.Errorf("error message %q does not name --no-minify as the reason", err.Error())
	}
}

// TestUpload_UnderLimitPassesThrough is the companion negative case: a
// source under MaxSourceBytes must upload normally, with the length check
// adding no friction to the ordinary path.
func TestUpload_UnderLimitPassesThrough(t *testing.T) {
	var putCodeCalls int
	dev := &fakeDevice{
		callFn: func(method string, params any) (any, error) {
			switch method {
			case List.String():
				return &ListResponse{Scripts: nil}, nil
			case Create.String():
				return &Status{Id: 1}, nil
			case PutCode.String():
				putCodeCalls++
				return &PutCodeResponse{Length: 5}, nil
			case SetConfig.String():
				return &Status{Id: 1}, nil
			default:
				t.Fatalf("unexpected RPC call %s(%v)", method, params)
				return nil, nil
			}
		},
	}

	id, err := Upload(context.Background(), types.ChannelDefault, dev, "small.js", []byte("print(1);"), false /* minify */)
	if err != nil {
		t.Fatalf("Upload() error = %v, want nil for a source well under the limit", err)
	}
	if id != 1 {
		t.Errorf("Upload() id = %d, want 1", id)
	}
	if putCodeCalls == 0 {
		t.Error("Script.PutCode was never called: upload did not proceed past the length check")
	}
}
