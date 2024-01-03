package main

import (
	"fmt"
	"net"
	"tapo"

	"github.com/spf13/cobra"
)

var switches []string = []string{
	"P100",
	"P110",
}

func init() {
	// listCmd.AddCommand(listTapoCmd)
	showCmd.AddCommand(showTapoCmd)
}

var showTapoCmd = &cobra.Command{
	Use:   "tapo <ip>",
	Short: "Show details of a given TP-LINK Tapo device",
	// Long:  `All software has versions. This is Hugo's`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		sw, err := tapo.NewSwitch(net.ParseIP(args[0]))
		if err != nil {
			panic(err)
		}
		fmt.Print(sw.GetInfo())
	},
}
