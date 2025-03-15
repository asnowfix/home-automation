package set

import (
	"fmt"
	"global"
	"homectl/options"
	"myhome"
	"mymqtt"
	"pkg/shelly"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var name string

func init() {
	setShellyCmd.Flags().StringVarP(&name, "name", "N", "", "set device name")
}

var setShellyCmd = &cobra.Command{
	Use:   "shelly",
	Short: "Set Shelly device attributes",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		shelly.Init(cmd.Context(), options.Flags.MqttTimeout)
		log := cmd.Context().Value(global.LogKey).(logr.Logger)
		var out any
		var err error

		identifier := args[0]

		out, err = myhome.TheClient.CallE(cmd.Context(), myhome.DeviceLookup, identifier)
		if err != nil {
			log.Error(err, "device lookup failed", "id", identifier)
			return err
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))
		device, ok := out.(*myhome.DeviceSummary)
		if !ok {
			log.Error(nil, "device not found", "id", identifier)
			return fmt.Errorf("device not found '%s'", identifier)
		}

		mc, err := mymqtt.GetClientE(cmd.Context())
		if err != nil {
			return err
		}
		sd := shelly.NewDeviceFromMqttId(cmd.Context(), log, device.Id, mc)

		out, err = sd.CallE(cmd.Context(), types.ChannelDefault, system.GetConfig.String(), nil)
		if err != nil {
			log.Error(err, "Unable to get device config", "id", device.Id, "host", device.Host)
			return err
		}
		config := out.(*system.Config)
		log.Info("Got device system config", "id", device.Id, "config", config)
		if name != "" {
			config.Device.Name = name
		}
		sd.CallE(cmd.Context(), types.ChannelDefault, system.SetConfig.String(), &config)
		return nil
	},
}
