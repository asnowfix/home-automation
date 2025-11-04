package reboot

import (
	"context"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	shellyapi "pkg/shelly"
	"pkg/shelly/shelly"
	"pkg/shelly/types"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "reboot <device-name-or-ip>",
	Short: "Reboot Shelly device",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, oneDeviceReboot, options.Args(args))
		return err
	},
}

func oneDeviceReboot(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shellyapi.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	
	fmt.Printf("Rebooting %s...\n", sd.Name())
	
	out, err := sd.CallE(ctx, via, shelly.Reboot.String(), nil)
	if err != nil {
		log.Error(err, "Unable to reboot device", "device", sd.Id())
		return nil, err
	}
	
	log.Info("Device reboot initiated, waiting for device to come back online", "device", sd.Id())
	fmt.Printf("Waiting for %s to come back online...\n", sd.Name())
	
	// Wait for device to go offline (give it 5 seconds to start rebooting)
	select {
	case <-time.After(5 * time.Second):
		// Continue
	case <-ctx.Done():
		fmt.Printf("\n✗ Interrupted while waiting for reboot\n")
		return nil, ctx.Err()
	}
	
	// Poll for device to come back online (up to 60 seconds)
	maxWait := 60 * time.Second
	pollInterval := 2 * time.Second
	timeout := time.After(maxWait)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("\n✗ Interrupted while waiting for device to come back online\n")
			return nil, ctx.Err()
		case <-timeout:
			// Timeout waiting for device
			fmt.Printf("⚠ Timeout waiting for %s to come back online (may still be rebooting)\n", sd.Name())
			log.Info("Timeout waiting for device to come back online", "device", sd.Id())
			return out, nil
		case <-ticker.C:
			// Try to get device status
			_, err := sd.CallE(ctx, via, shelly.GetStatus.String(), nil)
			if err == nil {
				// Device responded - it's back online
				fmt.Printf("✓ %s is back online\n", sd.Name())
				log.Info("Device is back online", "device", sd.Id())
				return out, nil
			}
			// Still offline, continue polling
		}
	}
}
