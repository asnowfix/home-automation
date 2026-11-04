package kvs

import (
	"context"
	"errors"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// TestGetValueNotFoundVersusFailure covers the distinction #663 depends on:
// ErrNotFound is returned only when the device itself answered that the key is
// missing. A timeout, a transport error, or a device error that is not
// "not found" says nothing about the key and must not look like a miss.
func TestGetValueNotFoundVersusFailure(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantNotFound bool
	}{
		{"device replied key not found", &types.RpcError{Code: -105, Message: "Key not found"}, true},
		{"device replied, wording varies", &types.RpcError{Code: -105, Message: "Argument 'key', value 'x' not found!"}, true},
		{"device replied with another error", &types.RpcError{Code: -1, Message: "Resource busy"}, false},
		{"timeout", errors.New("timeout waiting for response from shellypro1 (pompe)"), false},
		{"context deadline", context.DeadlineExceeded, false},
		{"transport error", errors.New("connection reset by peer"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetKvsCache()
			d := types.NewFakeDevice()
			d.IdValue = "device-nf-" + tc.name
			d.SetError(string(Get), tc.err)

			res, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "script/pool-pump.js")
			if err == nil {
				t.Fatalf("expected an error, got result %+v", res)
			}
			if got := errors.Is(err, ErrNotFound); got != tc.wantNotFound {
				t.Errorf("errors.Is(err, ErrNotFound) = %v, want %v (err: %v)", got, tc.wantNotFound, err)
			}
			if !errors.Is(err, tc.err) {
				t.Errorf("the underlying error was lost: errors.Is(err, %v) = false (err: %v)", tc.err, err)
			}
		})
	}
}

// TestGetValueCachedMissKeepsItsKind proves a cached miss replays as what it
// was. A transient failure is cached (as before, so an unreachable device does
// not cost a timeout per lookup) but must not later read back as ErrNotFound;
// a genuine not-found keeps reading back as one.
func TestGetValueCachedMissKeepsItsKind(t *testing.T) {
	t.Run("cached transient failure is not ErrNotFound", func(t *testing.T) {
		resetKvsCache()
		d := types.NewFakeDevice()
		d.IdValue = "device-cache-transient"
		d.SetError(string(Get), errors.New("timeout waiting for response"))

		if _, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "k"); errors.Is(err, ErrNotFound) {
			t.Fatalf("live transient failure reported as ErrNotFound: %v", err)
		}
		_, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "k")
		if err == nil {
			t.Fatal("expected the cached miss to be an error")
		}
		if errors.Is(err, ErrNotFound) {
			t.Errorf("cached transient failure replayed as ErrNotFound: %v", err)
		}
		if len(d.Calls) != 1 {
			t.Errorf("expected the second lookup to be served from cache (1 device call), got %d", len(d.Calls))
		}
	})

	t.Run("cached genuine not-found stays ErrNotFound", func(t *testing.T) {
		resetKvsCache()
		d := types.NewFakeDevice()
		d.IdValue = "device-cache-notfound"
		d.SetError(string(Get), &types.RpcError{Code: -105, Message: "Key not found"})

		for i := 0; i < 2; i++ {
			_, err := GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "k")
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("lookup %d: errors.Is(err, ErrNotFound) = false (err: %v)", i+1, err)
			}
		}
		if len(d.Calls) != 1 {
			t.Errorf("expected the second lookup to be served from cache (1 device call), got %d", len(d.Calls))
		}
	})
}
