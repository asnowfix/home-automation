package show

import (
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"reflect"

	"homectl/options"

	"github.com/spf13/cobra"
)

var showShellyCmd = &cobra.Command{
	Use:   "shelly",
	Short: "Show Shelly devices",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		identifier := args[0]
		log := hlog.Init()
		out, err := options.MyHomeClient.CallE("device.show", identifier)
		if err != nil {
			return err
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))
		device := out.(*myhome.Device)
		if options.Flags.Json {
			s, err := json.Marshal(device)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		} else {
			fmt.Println(device)
		}
		return nil
	},
}
