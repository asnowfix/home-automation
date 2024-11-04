package list

import (
	"devices"
	"encoding/json"
	"fmt"
	"hlog"
	"reflect"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List known devices connected on the home gateway",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Init()
		// devices.Init()

		hosts, err := devices.List(log)
		if err != nil {
			log.Error(err, "Failed to list devices")
			return err
		}
		log.Info("Found devices", "length", len(hosts), "type", reflect.TypeOf(hosts))
		out, err := json.Marshal(hosts)
		if err != nil {
			return err
		}
		fmt.Print(string(out))

		return nil
	},
}
