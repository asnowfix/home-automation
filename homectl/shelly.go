package main

import (
	"devices/shelly"
	"devices/shelly/mqtt"
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
var showWifiFlag bool

func init() {
	showShellyCmd.Flags().BoolVarP(&showAllFlag, "all", "a", false, "Show everything about (the) device(s).")
	showShellyCmd.Flags().BoolVarP(&showConfigFlag, "config", "c", false, "Show device configuration(s).")
	showShellyCmd.Flags().BoolVarP(&showStatusFlag, "status", "s", true, "Show device Status(s).")
	showShellyCmd.Flags().BoolVarP(&showWifiFlag, "wifi", "W", false, "Show device Wifi configuration(s).")
	showShellyCmd.Flags().BoolVarP(&showCloudFlag, "cloud", "C", false, "Show device Cloud configuration(s).")
	showShellyCmd.Flags().BoolVarP(&showMqttFlag, "mqtt", "M", false, "Show device MQTT configuration(s).")

	showCmd.AddCommand(showShellyCmd)
}

var showShellyCmd = &cobra.Command{
	Use:   "shelly",
	Short: "Show Shelly devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		InitLog()
		shelly.Init()

		if showAllFlag {
			showCloudFlag = true
			showConfigFlag = true
			showMqttFlag = true
			showStatusFlag = true
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
	}

	s.DeviceInfo = shelly.CallMethod(device, "Shelly", "GetDeviceInfo").(*shelly.DeviceInfo)
	// dc := shelly.CallMethod(device, "Shelly", "GetConfig").(*shelly.DeviceConfiguration)
	// ds := shelly.CallMethod(device, "Shelly", "GetStatus").(*shelly.DeviceStatus)

	if showMqttFlag {
		s.Mqtt.Config = shelly.CallMethod(device, "MQTT", "GetConfig").(*mqtt.Configuration)
		s.Mqtt.Status = shelly.CallMethod(device, "MQTT", "GetStatus").(*mqtt.Status)
	}

	out, err := json.Marshal(s)
	if err != nil {
		return err
	}
	fmt.Print(string(out))

	return nil
}
