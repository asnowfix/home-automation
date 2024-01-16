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
	showShellyCmd.LocalFlags().BoolVarP(&showAllFlag, "all", "a", false, "Show everything about (the) device(s).")
	showShellyCmd.LocalFlags().BoolVarP(&showConfigFlag, "config", "c", false, "Show device configuration(s).")
	showShellyCmd.LocalFlags().BoolVarP(&showStatusFlag, "status", "s", true, "Show device Status(s).")
	showShellyCmd.LocalFlags().BoolVarP(&showWifiFlag, "wifi", "W", false, "Show device Wifi configuration(s).")
	showShellyCmd.LocalFlags().BoolVarP(&showCloudFlag, "cloud", "C", false, "Show device Cloud configuration(s).")
	showShellyCmd.LocalFlags().BoolVarP(&showMqttFlag, "mqtt", "M", false, "Show device MQTT configuration(s).")

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
		DeviceInfo shelly.DeviceInfo `json:"info"`
		Mqtt       struct {
			Config mqtt.Configuration `json:"config"`
			Status mqtt.Status        `json:"status"`
		} `json:"mqtt"`
	}

	data, err := shelly.CallMethod(device, "Shelly.DeviceInfo")
	if err != nil {
		return err
	}
	s.DeviceInfo, _ = data.(shelly.DeviceInfo)

	// data, err := shelly.CallMethod(device, "Shelly.GetConfig")
	// if err != nil {
	// 	return err
	// }
	// s.Device.Config, _ = data.(shelly.Configuration)

	if showMqttFlag == true {
		if _, exists := device.Api["MQTT"]["GetConfig"]; exists {
			data, err := shelly.CallMethod(device, "MQTT.GetConfig")
			if err != nil {
				return err
			}
			s.Mqtt.Config = data.(mqtt.Configuration)
		}

		if _, exists := device.Api["MQTT"]["GetStatus"]; exists {
			data, err := shelly.CallMethod(device, "MQTT.GetStatus")
			if err != nil {
				return err
			}
			device.Api["MQTT"]["GetStatus"] = data.(mqtt.Status)
		}
	}

	out, err := json.Marshal(s)
	if err != nil {
		return err
	}
	fmt.Print(string(out))

	return nil
}
