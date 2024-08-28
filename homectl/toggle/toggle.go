package toggle

import (
	"devices"
	"devices/shelly"
	hlog "homectl/log"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "toggle",
	Short: "Toggle switch devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		hlog.Init()
		devices.Init()
		return shelly.Foreach(args, func(device *shelly.Device) (*shelly.Device, error) {
			_, err := shelly.CallE(device, "Switch", "Toggle", nil)
			return device, err
		})
	},
}
