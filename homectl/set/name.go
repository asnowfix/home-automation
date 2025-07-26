package set

import (
	"fmt"
	"global"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/system"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(nameCmd)
}

var nameCmd = &cobra.Command{
	Use:   "name",
	Short: "Set device name",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		identifier := args[0]
		name := args[1]

		ctx := cmd.Context()
		log := ctx.Value(global.LogKey).(logr.Logger)

		devices, err := myhome.TheClient.LookupDevices(ctx, identifier)
		if err != nil {
			log.Error(err, " No such device", "identifier", identifier)
			return err
		}

		device, err := shelly.NewDeviceFromSummary(ctx, log, (*devices)[0])
		if err != nil {
			log.Error(err, "Unable to create device from summary", "device", (*devices)[0])
			return err
		}
		sd, ok := device.(*shelly.Device)
		if !ok {
			return fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
		}

		log.Info("Setting name of device", "name", name, "device", sd.Id())
		_, err = system.SetName(ctx, sd, name)
		return err
	},
}
