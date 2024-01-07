package main

import (
	"devices/shelly"
	"encoding/json"
	"fmt"
	"log"
	"net"

	"github.com/spf13/cobra"
)

var showAllFlag bool
var showConfigFlag bool
var showStatusFlag bool
var showWifiFlag bool
var showCloudFlag bool
var showMqttFlag bool

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

		var Ip net.IP
		if len(args) > 0 {
			Ip = net.ParseIP(args[0])
			log.Default().Printf("Looking for Shelly with IP=%v", Ip)
			device, err := shelly.NewDevice(Ip)
			if err != nil {
				return err
			}
			out, err := json.Marshal(device)
			if err != nil {
				return err
			}
			fmt.Print(string(out))
		} else {
			log.Default().Printf("Looking for any Shelly device")
			devices, err := shelly.NewMdnsDevices()
			if err != nil {
				return err
			}
			out, err := json.Marshal(devices)
			if err != nil {
				return err
			}
			// fmt.Printf("Found %v devices '%v'\n", len(devices), reflect.TypeOf(device))
			fmt.Print(string(out))
		}
		return nil
	},
}
