package show

import (
	"devices/shelly"
	"devices/shelly/mqtt"
	"devices/shelly/sswitch"
	"devices/shelly/types"
	"encoding/json"
	"fmt"
	"hlog"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var showAllFlag bool
var showCloudFlag bool
var showConfigFlag bool
var showMqttFlag bool
var showStatusFlag bool
var showSwitchId int
var showWifiFlag bool

func init() {
	showShellyCmd.Flags().BoolVarP(&showAllFlag, "all", "a", false, "Show everything about (the) device(s).")
	showShellyCmd.Flags().BoolVarP(&showConfigFlag, "config", "c", false, "Show device configuration(s).")
	showShellyCmd.Flags().BoolVarP(&showStatusFlag, "status", "s", false, "Show device Status(s).")
	showShellyCmd.Flags().IntVarP(&showSwitchId, "switch", "S", -1, "Show status of this switch ID.")
	showShellyCmd.Flags().BoolVarP(&showWifiFlag, "wifi", "W", false, "Show device Wifi configuration(s).")
	showShellyCmd.Flags().BoolVarP(&showCloudFlag, "cloud", "C", false, "Show device Cloud configuration(s).")
	showShellyCmd.Flags().BoolVarP(&showMqttFlag, "mqtt", "M", false, "Show device MQTT configuration(s).")
}

var useHttpChannel bool

func init() {
	showShellyCmd.Flags().BoolVarP(&useHttpChannel, "http", "H", false, "Use HTTP channel to communicate with Shelly devices")
}

var showShellyCmd = &cobra.Command{
	Use:   "shelly",
	Short: "Show Shelly devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		shelly.Init(log)

		if showAllFlag {
			showCloudFlag = true
			showConfigFlag = true
			showMqttFlag = true
			showWifiFlag = true
		}

		ch := types.ChannelHttp
		if !useHttpChannel {
			ch = types.ChannelMqtt
		}
		shelly.Foreach(log, args, ch, showOneDevice)
		return nil
	},
}

func showOneDevice(log logr.Logger, via types.Channel, device *shelly.Device) (*shelly.Device, error) {

	var s struct {
		DeviceInfo *shelly.DeviceInfo `json:"info"`
		Mqtt       struct {
			Config *mqtt.Configuration `json:"config,omitempty"`
			Status *mqtt.Status        `json:"status,omitempty"`
		} `json:"mqtt,omitempty"`
		Switch struct {
			Config *sswitch.Configuration `json:"config,omitempty"`
			Status *sswitch.Status        `json:"status,omitempty"`
		} `json:"switch,omitempty"`
	}

	s.DeviceInfo = device.Info
	// dc := shelly.CallMethod(device, "Shelly", "GetConfig").(*shelly.DeviceConfiguration)
	// ds := shelly.CallMethod(device, "Shelly", "GetStatus").(*shelly.DeviceStatus)

	channel := types.ChannelMqtt
	if useHttpChannel {
		channel = types.ChannelHttp
	}

	if showMqttFlag {
		s.Mqtt.Config = device.Call(channel, "Mqtt", "GetConfig", nil, &mqtt.ConfigResults{}).(*mqtt.Configuration)
		s.Mqtt.Status = device.Call(channel, "Mqtt", "GetStatus", nil, &mqtt.Status{}).(*mqtt.Status)
	}

	if showSwitchId >= 0 {
		sr := make(map[string]interface{})
		sr["id"] = showSwitchId
		s.Switch.Config = device.Call(channel, "Switch", "GetConfig", sr, &sswitch.Configuration{}).(*sswitch.Configuration)
		s.Switch.Status = device.Call(channel, "Switch", "GetStatus", sr, &sswitch.Status{}).(*sswitch.Status)
	}

	out, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(out))

	return device, nil
}
