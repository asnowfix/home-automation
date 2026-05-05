package script

import (
	"context"
	"fmt"
	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/asnowfix/go-shellies/devices"
	"github.com/asnowfix/go-shellies"
	pkgscript "github.com/asnowfix/go-shellies/script"
	"github.com/asnowfix/go-shellies/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var snapshotDeviceFile string

func init() {
	Cmd.AddCommand(snapshotCmd)
	snapshotCmd.Flags().StringVarP(&snapshotDeviceFile, "device-file", "D", "device.json", "Output file for device state (KVS and component status)")
}

var snapshotCmd = &cobra.Command{
	Use:     "snapshot <device-identifier>",
	Aliases: []string{"s"},
	Short:   "Snapshot a device's KVS and component status into a device file for use with 'script run'",
	Long: `Snapshot connects to a live Shelly device and writes its current state
to a device file (default: device.json).

The resulting file can be passed to 'script run' via --device-file so the
local JavaScript VM sees realistic initial conditions:

  myhome ctl shelly script snapshot my-device
  myhome ctl shelly script run pool-pump.js

Captured fields:
  - kvs:              all key-value pairs stored on the device
  - component_status: point-in-time status of all components
                      (switch:0, input:0, sys, mqtt, ...)
  - storage:          always empty (Script.storage is not accessible via RPC)

Note: component_status is a snapshot; the local VM does not stay in sync
with the real device after the file is written.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		_, err := myhome.Foreach(cmd.Context(), log, args[0], options.Via, doSnapshot, options.Args(args))
		return err
	},
}

func doSnapshot(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, _ []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	fmt.Printf("Snapshotting %s...\n", sd.Name())

	state, err := pkgscript.FetchDeviceState(ctx, via, sd)
	if err != nil {
		fmt.Printf("✗ Failed to fetch state from %s: %v\n", sd.Name(), err)
		return nil, err
	}

	if err := pkgscript.SaveDeviceState(log, snapshotDeviceFile, state); err != nil {
		fmt.Printf("✗ Failed to save %s: %v\n", snapshotDeviceFile, err)
		return nil, err
	}

	fmt.Printf("✓ Saved to %s (%d KVS keys, %d components)\n",
		snapshotDeviceFile, len(state.KVS), len(state.ComponentStatus))
	return state, nil
}
