package list

import (
	"encoding/json"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"reflect"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List known devices",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		log := hlog.Logger

		out, err := myhome.TheClient.CallE(cmd.Context(), myhome.DeviceList, nil)
		if err != nil {
			return err
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))
		devices := out.(*myhome.Devices)
		var s []byte
		if options.Flags.Json {
			s, err = json.Marshal(devices)
		} else {
			s, err = yaml.Marshal(devices)
		}
		if err != nil {
			return err
		}
		fmt.Println(string(s))
		return nil
	},
}
