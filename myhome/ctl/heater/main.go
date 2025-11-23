package heater

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "heater",
	Short: "Manage heater script configuration on Shelly devices",
	Long:  "Configure, monitor, and manage the heater.js script on Shelly devices with KVS-based configuration.",
}

// Shared KVS keys used by all heater commands
var heaterKVSKeys = []string{
	"script/heater/enable-logging",
	"script/heater/set-point",
	"script/heater/min-internal-temp",
	"script/heater/cheap-start-hour",
	"script/heater/cheap-end-hour",
	"script/heater/poll-interval-ms",
	"script/heater/preheat-hours",
	"normally-closed",
	"script/heater/internal-temperature-topic",
	"script/heater/external-temperature-topic",
	"script/heater/room-id",
}

func init() {
	Cmd.AddCommand(setupCmd)
	Cmd.AddCommand(statusCmd)
	Cmd.AddCommand(deleteCmd)
	Cmd.AddCommand(updateCmd)
}
