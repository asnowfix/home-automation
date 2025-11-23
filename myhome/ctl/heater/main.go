package heater

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "heater",
	Short: "Manage heater script configuration on Shelly devices",
	Long:  "Configure, monitor, and manage the heater.js script on Shelly devices with KVS-based configuration.",
}

// heaterKVSKeys must match CONFIG_SCHEMA in heater.js
// Source: internal/shelly/scripts/heater.js:11-79
//
// Each key corresponds to a field in CONFIG_SCHEMA with the mapping shown in comments.
// The key format is either "script/heater/<key>" or unprefixed (for normallyClosed).
// Changes to CONFIG_SCHEMA must be reflected here and validated by TestHeaterKVSKeysMatchJSSchema.
var heaterKVSKeys = []string{
	"script/heater/enable-logging",             // CONFIG_SCHEMA.enableLogging
	"script/heater/set-point",                  // CONFIG_SCHEMA.setpoint
	"script/heater/min-internal-temp",          // CONFIG_SCHEMA.minInternalTemp
	"script/heater/cheap-start-hour",           // CONFIG_SCHEMA.cheapStartHour
	"script/heater/cheap-end-hour",             // CONFIG_SCHEMA.cheapEndHour
	"script/heater/poll-interval-ms",           // CONFIG_SCHEMA.pollIntervalMs
	"script/heater/preheat-hours",              // CONFIG_SCHEMA.preheatHours
	"normally-closed",                          // CONFIG_SCHEMA.normallyClosed (unprefixed: true)
	"script/heater/internal-temperature-topic", // CONFIG_SCHEMA.internalTemperatureTopic
	"script/heater/external-temperature-topic", // CONFIG_SCHEMA.externalTemperatureTopic
	"script/heater/room-id",                    // CONFIG_SCHEMA.roomId
}

func init() {
	Cmd.AddCommand(setupCmd)
	Cmd.AddCommand(statusCmd)
	Cmd.AddCommand(deleteCmd)
	Cmd.AddCommand(updateCmd)
	Cmd.AddCommand(listCmd)
}
