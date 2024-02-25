package list

import (
	"devices"
	"encoding/json"
	"fmt"
	hlog "homectl/log"
	"log"
	"reflect"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List known devices connected on the home gateway",
	RunE: func(cmd *cobra.Command, args []string) error {
		hlog.Init()
		devices.Init()

		hosts, err := devices.List()
		if err != nil {
			log.Default().Print(err)
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
