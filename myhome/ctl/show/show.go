package show

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/ctl/options"

	"gopkg.in/yaml.v3"

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

		client, err := myhome.ClientFromContext(cmd.Context())
		if err != nil {
			return err
		}

		device, err := myhome.Call[*myhome.DeviceShowParams, *myhome.Device](cmd.Context(), client, myhome.DeviceShow, &myhome.DeviceShowParams{Identifier: args[0]})
		if err != nil {
			return err
		}
		log.Info("result", "device", device, "type", reflect.TypeOf(device))
		if options.Flags.Json {
			s, err := json.Marshal(device)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		} else {
			s, err := yaml.Marshal(device)
			if err != nil {
				return err
			}
			fmt.Println(string(s))
		}
		return nil
	},
}
