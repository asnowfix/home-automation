package set

import (
	"fmt"
	"global"
	"homectl/options"
	"myhome"
	"mymqtt"
	"mynet"
	"net"
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

		var sd *shelly.Device
		ip := net.ParseIP(device.Host)
		if ip != nil && mynet.IsSameNetwork(log, ip) == nil {
			log.Info("Using IP to reach device", "host", device.Host)
			sd = shelly.NewDeviceFromIp(cmd.Context(), log, ip)
		} else {
			log.Info("Using MQTT to reach device", "id", identifier)
			sd = shelly.NewDeviceFromMqttId(cmd.Context(), log, identifier, mymqtt.GetClient(cmd.Context()))
		}
		// TODO implement
		//sd := shelly.NewDeviceFromDeviceSummary(cmd.Context(), log, device)

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

		configReq := system.SetConfigRequest{
			Config: *config,
		}
		log.Info("Setting device system config", "id", device.Id, "host", device.Host, "config", config)
		out, err = sd.CallE(cmd.Context(), types.ChannelDefault, system.SetConfig.String(), &configReq)
		if err != nil {
			log.Error(err, "Unable to set device config", "id", device.Id, "host", device.Host)
			return err
		}
		configRes := out.(*system.SetConfigResponse)
		if configRes.RestartRequired {
			sd.CallE(cmd.Context(), types.ChannelDefault, shelly.Reboot.String(), nil)
		}
		return nil
	},
}
