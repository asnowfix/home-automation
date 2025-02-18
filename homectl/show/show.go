package show

import (
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"reflect"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(showShellyCmd)
	Cmd.AddCommand(showTapoCmd)
}

var Cmd = &cobra.Command{
	Use:   "show",
	Short: "Show devices",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger

		out, err := options.MyHomeClient.CallE(cmd.Context(), "device.show", args[0])
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
