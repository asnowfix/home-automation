package daemon

import (
	"context"
	"global"
	"homectl/options"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(runCmd)

	runCmd.Flags().BoolVarP(&disableDeviceManager, "disable-device-manager", "D", false, "Disable the device manager")
	runCmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	runCmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", 5*time.Second, "Timeout for MQTT operations")
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
