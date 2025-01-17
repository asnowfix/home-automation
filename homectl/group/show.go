package group

import (
	"encoding/json"
	"fmt"
	"homectl/options"
	"myhome/devices"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(showCmd)
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show device groups",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		out, err := myhomeClient.CallE("group.show", name)
		if err != nil {
			panic(err)
		}
		devices := out.(*[]*devices.Device)
		if options.Flags.Json {
			s, err := json.Marshal(devices)
			if err != nil {
				panic(err)
			}
			fmt.Println(string(s))
		} else {
			for _, device := range *devices {
				fmt.Println(device)
			}
		}
	},
}
