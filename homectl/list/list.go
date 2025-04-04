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
	Args:  cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		var out any

		log := hlog.Logger
		ctx := cmd.Context()

		if len(args) == 1 {
			out, err = myhome.TheClient.CallE(ctx, myhome.DeviceLookup, args[0])
		} else {
			out, err = myhome.TheClient.CallE(ctx, myhome.DeviceList, nil)
		}
		if err != nil {
			return err
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))
		devices, ok := out.(*myhome.Devices)
		if !ok {
			return fmt.Errorf("expected myhome.Devices, got %T", reflect.TypeOf(out))
		}
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
