package list

import (
	"encoding/json"
	"fmt"
	"homectl/options"

	"myhome/devices"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List known devices",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := options.MyHomeClient.CallE("devices.list", nil)
		if err != nil {
			return err
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
		return nil
	},
}
