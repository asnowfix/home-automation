package shelly

import (
	"hlog"

	"github.com/spf13/cobra"

	"devices/shelly"
	"devices/shelly/mqtt"
)

var mqttCmd = &cobra.Command{
	Use:   "mqtt",
	Short: "Set Shelly devices MQTT configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		hlog.Init()
		return shelly.Foreach(args, mqtt.Setup)
	},
}
