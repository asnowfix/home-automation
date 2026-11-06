package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"

	"github.com/go-logr/logr"
)

// pool == nil means either "tracking not enabled" or "enabled but the device was
// unreachable at startup"; only the first may report ErrPoolNotEnabled.
func TestPoolGetStatusNilPool(t *testing.T) {
	_, err := NewPoolRPCHandler(logr.Discard(), nil, false).handleGetStatus(context.Background(), nil)
	if !errors.Is(err, myhome.ErrPoolNotEnabled) {
		t.Errorf("disabled: err = %v, want ErrPoolNotEnabled", err)
	}
	_, err = NewPoolRPCHandler(logr.Discard(), nil, true).handleGetStatus(context.Background(), nil)
	if err == nil || errors.Is(err, myhome.ErrPoolNotEnabled) {
		t.Errorf("enabled but unreachable: err = %v, want a non-ErrPoolNotEnabled error", err)
	}
}
