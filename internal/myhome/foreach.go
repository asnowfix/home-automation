package myhome

import (
	"context"
	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
)

// Foreach calls a function for each device that matches the given name.
// The function is called with the context, logger, channel, device, and additional arguments.
// The function should return a result and an error.
// The Foreach function aggregates the results of all the function calls and returns a single result.
// If any of the function calls return an error, the Foreach function will return that error.
func Foreach(ctx context.Context, log logr.Logger, name string, via types.Channel, fn func(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error), args []string) (any, error) {
	return ForeachWith(ctx, log, name, via, fn, args, shelly.ForeachOptions{})
}

// ForeachWith is Foreach with options, e.g. a short reachability probe so that
// known-but-silent devices are listed as unreachable instead of failing the run.
func ForeachWith(ctx context.Context, log logr.Logger, name string, via types.Channel, fn func(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error), args []string, opts shelly.ForeachOptions) (any, error) {
	// Get a list of devices that match the given name.
	devices, err := TheClient.LookupDevices(ctx, name)
	if err != nil {
		return nil, err
	}

	// Call the given function for each device in the list.
	return shelly.ForeachWith(ctx, log, *devices, via, fn, args, opts)
}
