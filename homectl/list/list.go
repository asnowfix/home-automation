package list

import (
	"devices/sfr"
	"encoding/json"
	"fmt"
	"log"
	"reflect"

	"github.com/spf13/cobra"
)

func init() {
}

var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List known devices connected on the home gateway",
	RunE: func(cmd *cobra.Command, args []string) error {
		hosts, err := sfr.ListDevices()
		if err != nil {
			return err
		}
		log.Default().Printf("Found %v devices '%v'\n", len(hosts), reflect.TypeOf(hosts))
		out, err := json.Marshal(hosts)
		if err != nil {
			return err
		}
		fmt.Print(string(out))

		return nil
	},
}