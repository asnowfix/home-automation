package show

import (
	"devices/shelly"
	"devices/shelly/mqtt"
	"devices/shelly/sswitch"
	"encoding/json"
	"fmt"
	"log"
	"reflect"

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

var showShellyCmd = &cobra.Command{
	Use:   "shelly",
	Short: "Show Shelly devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		if showAllFlag {
			showCloudFlag = true
			showConfigFlag = true
			showMqttFlag = true
			showStatusFlag = true
			showSwitchFlag = true
			showWifiFlag = true
		}

		if len(args) > 0 {
			log.Default().Printf("Looking for Shelly device %v", args[0])
			device, err := shelly.NewDevice(args[0])
			if err != nil {
				return err
			}
			showOneDevice(device)
		} else {
			log.Default().Printf("Looking for any Shelly device")
			devices, err := shelly.NewMdnsDevices()
			if err != nil {
				return err
			}
			log.Default().Printf("Found %v devices '%v'\n", len(*devices), reflect.TypeOf(*devices))
			for _, device := range *devices {
				showOneDevice(device)
			}
		}

		return nil
	},
}

func showOneDevice(device *shelly.Device) error {

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
		s.Mqtt.Config = shelly.CallMethod(device, "Mqtt", "GetConfig").(*mqtt.Configuration)
		s.Mqtt.Status = shelly.CallMethod(device, "Mqtt", "GetStatus").(*mqtt.Status)
	}

	if showSwitchFlag {
		s.Switch.Config = shelly.CallMethod(device, "Switch", "GetConfig").(*sswitch.Configuration)
		s.Switch.Status = shelly.CallMethod(device, "Switch", "GetStatus").(*sswitch.Status)
	}

	out, err := json.Marshal(s)
	if err != nil {
		return err
	}
	fmt.Print(string(out))

	return nil
}