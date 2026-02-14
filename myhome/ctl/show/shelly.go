package show

import (
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"

	"myhome/ctl/options"

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
		var err error
		ctx := cmd.Context()

		identifier := args[0]
		log := hlog.Logger

		// Use LookupDevices to support glob patterns
		devices, err := myhome.TheClient.LookupDevices(ctx, identifier)
		if err != nil {
			return err
		}

		if devices == nil || len(*devices) == 0 {
			return fmt.Errorf("no devices found matching pattern: %s", identifier)
		}

		log.Info("found devices", "count", len(*devices), "pattern", identifier)

		// For long output, fetch full device details for each matched device
		var show any
		if long {
			// Fetch full device info for each matched device
			fullDevices := make([]*myhome.Device, 0, len(*devices))
			for _, dev := range *devices {
				out, err := myhome.TheClient.CallE(ctx, myhome.DeviceShow, &myhome.DeviceShowParams{Identifier: dev.Id()})
				if err != nil {
					log.Error(err, "failed to fetch device details", "id", dev.Id())
					continue
				}
				device, ok := out.(*myhome.Device)
				if !ok {
					log.Error(fmt.Errorf("unexpected type: expected *myhome.Device"), "id", dev.Id())
					continue
				}
				fullDevices = append(fullDevices, device)
			}
			show = fullDevices
		} else {
			// Show device summaries (default)
			show = *devices
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
