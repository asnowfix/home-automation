package open

import (
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"os/exec"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var Cmd = &cobra.Command{
	Use:   "open",
	Short: "Open a Shelly device in a web browser",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		name := args[0]

		devices, err := myhome.TheClient.LookupDevices(cmd.Context(), name)
		if err != nil {
			return err
		}
		if len(*devices) == 0 {
			return fmt.Errorf("no devices found with name %s", name)
		}
		device := (*devices)[0]

		var s []byte
		if options.Flags.Json {
			s, err = json.Marshal(device)
		} else {
			s, err = yaml.Marshal(device)
		}
		if err != nil {
			return err
		}
		fmt.Println(string(s))

		sh := fmt.Sprintf("open http://%s", device.Host())
		log.Info("Executing command", "command", sh)
		return exec.Command("sh", "-c", sh).Run()
	},
}
