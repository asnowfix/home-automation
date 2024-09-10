package show

import (
	"devices/shelly"
	"devices/shelly/mqtt"
	"devices/shelly/sswitch"
	"encoding/json"
	"fmt"
	"hlog"

	"github.com/spf13/cobra"
)

var showAllFlag bool
var showCloudFlag bool
var showConfigFlag bool
var showMqttFlag bool
var showStatusFlag bool
var showSwitchFlag bool
var showWifiFlag bool

func init() {
	showShellyCmd.Flags().BoolVarP(&showAllFlag, "all", "a", false, "Show everything about (the) device(s).")
	showShellyCmd.Flags().BoolVarP(&showConfigFlag, "config", "c", false, "Show device configuration(s).")
	showShellyCmd.Flags().BoolVarP(&showStatusFlag, "status", "s", false, "Show device Status(s).")
	showShellyCmd.Flags().BoolVarP(&showSwitchFlag, "switch", "S", false, "Show switch Status(s).")
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
		hlog.Init()
		if showAllFlag {
			showCloudFlag = true
			showConfigFlag = true
			showMqttFlag = true
			showSwitchFlag = true
			showWifiFlag = true
		}

		shelly.Foreach(args, showOneDevice)
		return nil
	},
}

func showOneDevice(device *shelly.Device) (*shelly.Device, error) {

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

	if showMqttFlag {
		s.Mqtt.Config = shelly.Call(device, "Mqtt", "GetConfig", nil).(*mqtt.Configuration)
		s.Mqtt.Status = shelly.Call(device, "Mqtt", "GetStatus", nil).(*mqtt.Status)
	}

	if showSwitchFlag {
		s.Switch.Config = shelly.Call(device, "Switch", "GetConfig", nil).(*sswitch.Configuration)
		s.Switch.Status = shelly.Call(device, "Switch", "GetStatus", nil).(*sswitch.Status)
	}

	out, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	fmt.Print(string(out))

	return device, nil
}
