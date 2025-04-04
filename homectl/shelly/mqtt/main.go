package mqtt

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "mqtt",
	Short: "Shelly devices MQTT configuration & status",
	Args:  cobra.NoArgs,
}
