package daemon

import (
	"github.com/spf13/cobra"
)

func init() {
}

var disableDeviceManager bool

var Cmd = &cobra.Command{
	Use:   "daemon",
	Short: "MyHome Daemon",
	Long:  "MyHome Daemon, with embedded MQTT broker and persistent device manager",
	Args:  cobra.NoArgs,
}
