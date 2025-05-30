package set

import (
	"fmt"
	"global"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
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

		device := shelly.NewDeviceFromSummary(ctx, log, (*devices)[0])
		sd, ok := device.(*shelly.Device)
		if !ok {
			return fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
		}

		log.Info("Getting system config of device", "name", name, "device", sd.Id())

		// out, err := sd.CallE(ctx, types.ChannelDefault, system.GetConfig.String(), nil)
		// if err != nil {
		// 	log.Error(err, "Unable to get device system config", "name", name, "device", sd.Id())
		// 	return err
		// }
		// c, ok := out.(*system.Config)
		// if !ok {
		// 	err = fmt.Errorf("invalid response to get device config: type='%v' expected='*system.Config'", reflect.TypeOf(out))
		// 	log.Error(err, "Invalid response to get device config", "name", name, "device", sd.Id())
		// 	return err
		// }

		log.Info("Setting name of device", "name", name, "device", sd.Id())
		// c.Device.Name = name

		out, err := sd.CallE(ctx, types.ChannelDefault, system.SetConfig.String(), &system.SetConfigRequest{
			Config: system.Config{
				Device: &system.DeviceConfig{
					Name: name,
				},
			},
		})
		if err != nil {
			log.Error(err, "Unable to set device name", "name", name, "device", sd.Id())
			return err
		}

		cres, ok := out.(*system.SetConfigResponse)
		if !ok {
			err = fmt.Errorf("invalid response to set device name: type='%v' expected='*system.SetConfigResponse'", reflect.TypeOf(out))
			log.Error(err, "Invalid response to set device name", "name", name, "device", sd.Id())
			return err
		}

		log.Info("Set device name", "name", name, "device", sd.Id(), "restart_required", cres.RestartRequired)
		return nil
	},
}
