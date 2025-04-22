package myhome

import (
	"context"
	"homectl/options"
	"pkg/shelly"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

func Foreach(ctx context.Context, log logr.Logger, name string, via types.Channel, fn func(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error), args []string) (any, error) {
	devices, err := TheClient.LookupDevices(ctx, name)
	if err != nil {
		return nil, err
	}

	out, err := shelly.Foreach(ctx, log, *devices, options.Via, fn, args)
	return out, err
}
