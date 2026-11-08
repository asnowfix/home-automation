package sswitch

import (
	"context"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

// prime fills the polarity cache the way a refresh does: one KVS.Get through a stub device.
func prime(t *testing.T, id, value string, fail bool) *types.FakeDevice {
	t.Helper()
	d := types.NewFakeDevice()
	d.IdValue = id
	if fail {
		d.SetError(string(kvs.Get), &types.RpcError{Code: 404, Message: "Key not found"})
	} else {
		d.SetResult(string(kvs.Get), &kvs.GetResponse{Value: value})
	}
	_, _ = kvs.GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "normally-closed")
	return d
}

func TestOnValueCached(t *testing.T) {
	if _, known := OnValueCached("sw-unprimed", 0); known {
		t.Error("unprimed device must be unknown")
	}

	prime(t, "sw-nc", "true", false)
	if on, known := OnValueCached("sw-nc", 0); !known || on {
		t.Errorf("normally-closed switch 0: on=%v known=%v, want false,true", on, known)
	}
	prime(t, "sw-multi", "[false,true]", false)
	if on, known := OnValueCached("sw-multi", 1); !known || on {
		t.Errorf("switch 1 nc: on=%v known=%v, want false,true", on, known)
	}
	if on, known := OnValueCached("sw-multi", 0); !known || !on {
		t.Errorf("switch 0 not nc: on=%v known=%v, want true,true", on, known)
	}
	if _, known := OnValueCached("sw-multi", 5); known {
		t.Error("out-of-range switch id must be unknown, not a panic")
	}
	prime(t, "sw-bad0", "garbage", false)
	if _, known := OnValueCached("sw-bad0", 0); known {
		t.Error("unparsable value must be unknown for switch 0 too")
	}
	prime(t, "sw-arr0", "[true,false]", false)
	if on, known := OnValueCached("sw-arr0", 0); !known || on {
		t.Errorf("array form, switch 0 nc: on=%v known=%v, want false,true", on, known)
	}
	prime(t, "sw-bad", "not-json", false)
	if _, known := OnValueCached("sw-bad", 2); known {
		t.Error("unparsable array must be unknown")
	}
}

func TestOnValueCachedKvsRevInvalidates(t *testing.T) {
	prime(t, "sw-rev", "true", false)
	kvs.ObserveRevision(logr.Discard(), "sw-rev", 10)
	if _, known := OnValueCached("sw-rev", 0); !known {
		t.Fatal("primed device must be known")
	}
	if !kvs.ObserveRevision(logr.Discard(), "sw-rev", 11) {
		t.Fatal("external kvs_rev bump must invalidate")
	}
	if _, known := OnValueCached("sw-rev", 0); known {
		t.Error("polarity must be unknown after a kvs_rev change, until re-primed")
	}
}

func TestOnValueCachedAbsentAndTransient(t *testing.T) {
	prime(t, "sw-absent", "", true)
	if on, known := OnValueCached("sw-absent", 0); !known || !on {
		t.Errorf("key absent on device: on=%v known=%v, want true,true", on, known)
	}
	d := types.NewFakeDevice()
	d.IdValue = "sw-timeout"
	d.SetError(string(kvs.Get), context.DeadlineExceeded)
	_, _ = kvs.GetValue(context.Background(), logr.Discard(), types.ChannelDefault, d, "normally-closed")
	if _, known := OnValueCached("sw-timeout", 0); known {
		t.Error("a timed-out lookup is not a fact about the key: must stay unknown")
	}
}
