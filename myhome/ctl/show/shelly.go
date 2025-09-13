package show

import (
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	shellyapi "pkg/shelly"
	"reflect"

	"homectl/options"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var long bool

func init() {
	showShellyCmd.PersistentFlags().BoolVarP(&long, "long", "l", false, "long output")
}

var showShellyCmd = &cobra.Command{
	Use:   "shelly",
	Short: "Show Shelly devices",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		shellyapi.Init(hlog.Logger, options.Flags.MqttTimeout)

		var out any
		var err error
		var device *myhome.Device

		identifier := args[0]
		log := hlog.Logger
		ctx := cmd.Context()

		out, err = myhome.TheClient.CallE(ctx, myhome.DeviceShow, identifier)
		if err != nil {
			return err
		}
		var ok bool
		device, ok = out.(*myhome.Device)
		if !ok {
			return fmt.Errorf("expected myhome.Device, got %T", out)
		}
		log.Info("result", "out", out, "type", reflect.TypeOf(out))

		var show any = device
		if !long {
			show = device.DeviceSummary
		}

		var s []byte
		if options.Flags.Json {
			s, err = json.Marshal(show)
		} else {
			s, err = yaml.Marshal(show)
		}
		if err != nil {
			return err
		}
		fmt.Println(string(s))
		return nil
	},
}
