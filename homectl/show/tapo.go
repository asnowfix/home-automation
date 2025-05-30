package show

import (
	"fmt"
	"hlog"
	"net"
	"tapo"

	"github.com/spf13/cobra"
)

var Switches []string = []string{
	"P100",
	"P110",
}

var showTapoCmd = &cobra.Command{
	Use:   "tapo <ip>",
	Short: "Show details of a given TP-LINK Tapo device",
	// Long:  `All software has versions. This is Hugo's`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		log := hlog.Logger
		sw, err := tapo.NewSwitch(log, net.ParseIP(args[0]))
		if err != nil {
			panic(err)
		}
		fmt.Print(sw.GetInfo())
	},
}
