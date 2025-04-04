package myhome

import (
	"context"
	"homectl/options"
	"pkg/shelly"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

func Foreach(ctx context.Context, log logr.Logger, name string, via types.Channel, fn func(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error), args []string) error {
	devices, err := TheClient.LookupDevices(ctx, name)
	if err != nil {
		return err
	}
	ids := make([]string, len(devices.Devices))
	for i, d := range devices.Devices {
		ids[i] = d.Id
	}

	return shelly.Foreach(ctx, log, ids, options.Via, fn, args)
}
