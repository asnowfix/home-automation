package list

import (
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"reflect"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List known devices",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		out, err := options.MyHomeClient.CallE("device.list", nil)
		if err != nil {
			return err
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))
		devices := out.(*myhome.Devices)
		if options.Flags.Json {
			s, err := json.Marshal(devices)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		} else {
			for _, device := range devices.Devices {
				fmt.Println(device)
			}
		}
		return nil
	},
}
