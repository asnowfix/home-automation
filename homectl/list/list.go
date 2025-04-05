package list

import (
	"encoding/json"
	"fmt"
	"homectl/options"
	"myhome"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List known devices",
	Args:  cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := "*"
		if len(args) == 1 {
			name = args[0]
		}

		devices, err := myhome.TheClient.LookupDevices(cmd.Context(), name)
		if err != nil {
			return err
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
