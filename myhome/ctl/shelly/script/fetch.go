package script

import (
	"context"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"os"
	"path/filepath"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(fetchCtl)
}

var fetchCtl = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch scripts from Shelly device(s)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		_, err := myhome.Foreach(cmd.Context(), log, args[0], options.Via, fetchFromOneDevice, options.Args(args))
		return err
	},
}

func fetchFromOneDevice(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	if len(args) == 1 {
		scripts, err := script.DeviceStatus(ctx, sd, via)
		if err != nil {
			return nil, err
		}
		for _, script := range scripts {
			log.Info("Fetching script", "name", script.Name, "id", script.Id)
			err := fetchOneScript(ctx, log, via, device, []string{script.Name})
			if err != nil {
				log.Error(err, "Unable to fetch script from device (skipping)", "name", script.Name, "id", script.Id, "device", device.Id())
			}
		}
		return nil, nil
	} else {
		// fetch just the script name that is given as second argument
		return nil, fetchOneScript(ctx, log, via, device, args)
	}
}

func fetchOneScript(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) error {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	loaded, err := script.ListLoaded(ctx, via, sd)
	if err != nil {
		return err
	}
	for _, l := range loaded {
		if l.Name == args[0] {
			log.Info("Fetching script", "name", args[0], "id", l.Id)
			code, err := script.Download(ctx, via, sd, args[0], l.Id)
			if err != nil {
				log.Error(err, "Unable to get code", "name", args[0])
				return err
			}
			// store code string in file by name, in a folder named by the device id
			os.MkdirAll(sd.Id(), 0755)
			os.WriteFile(filepath.Join(sd.Id(), args[0]), []byte(code), 0644)
			return nil
		}
	}
	return nil
}
