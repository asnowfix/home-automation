package main

import (
	"devices/shelly"
	"encoding/json"
	"fmt"
	"log"
	"net"

	"github.com/spf13/cobra"
)

func init() {
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
		} else {
			Ip = net.IPv4zero
		}
		log.Default().Printf("Looking for Shelly with IP=%v\n", Ip)
		devices, err := shelly.MyShellies(Ip)
		if err != nil {
			return err
		}
		if len(args) > 0 {
			out, err := json.Marshal((*devices)[args[0]])
			if err != nil {
				return err
			}
			fmt.Print(string(out))
		} else {
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
