package daemon

import (
	"context"
	"global"
	"homectl/options"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(runCmd)

	runCmd.Flags().BoolVarP(&disableDeviceManager, "disable-device-manager", "D", false, "Disable the device manager")
	runCmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	runCmd.PersistentFlags().DurationVarP(&options.Flags.MdnsTimeout, "mdns-timeout", "M", options.MDNS_LOOKUP_DEFAULT_TIMEOUT, "Timeout for mDNS lookups")
	runCmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", options.MQTT_DEFAULT_TIMEOUT, "Timeout for MQTT operations")
	runCmd.PersistentFlags().DurationVarP(&options.Flags.MqttGrace, "mqtt-grace", "G", options.MQTT_DEFAULT_GRACE, "MQTT disconnection grace period")
	runCmd.PersistentFlags().DurationVarP(&options.Flags.RefreshInterval, "refresh-interval", "R", options.DEVICE_REFRESH_INTERVAL, "Known devices refresh interval")
	runCmd.PersistentFlags().StringVarP(&options.Flags.EventsDir, "events-dir", "E", "", "Directory to write received MQTT events as JSON files")
	runCmd.PersistentFlags().IntVarP(&options.Flags.ProxyPort, "proxy-port", "p", 8080, "Reverse proxy listen port (default 8080)")
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run MyHome in foreground",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cancel := ctx.Value(global.CancelKey).(context.CancelFunc)
		log := ctx.Value(global.LogKey).(logr.Logger)

		daemon := NewDaemon(ctx, cancel, log)
		log.Info("Running in foreground")
		return daemon.Run()
	},
}
