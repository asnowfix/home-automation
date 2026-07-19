package myhome

import (
	"context"

	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
)

// Foreach calls a function for each device that matches the given name.
// The function is called with the context, logger, channel, device, and additional arguments.
// The function should return a result and an error.
// The Foreach function aggregates the results of all the function calls and returns a single result.
// If any of the function calls return an error, the Foreach function will return that error.
func Foreach(ctx context.Context, log logr.Logger, name string, via types.Channel, fn shelly.Do, args []string) (any, error) {
	// Get a list of devices that match the given name.
	found, err := TheClient.LookupDevices(ctx, name)
	if err != nil {
		return nil, err
	}

	// pkg/devices.Device already satisfies shelly.Summary structurally, but
	// Go does not implicitly convert []devices.Device to []shelly.Summary —
	// each element must be reboxed into the interface pkg/shelly expects.
	summaries := make([]shelly.Summary, len(*found))
	for i, d := range *found {
		summaries[i] = d
	}

	// Call the given function for each device in the list.
	out, err := shelly.Foreach(ctx, log, summaries, via, fn, args)
	return out, err
}
