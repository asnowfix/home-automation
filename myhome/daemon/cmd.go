package daemon

import (
	"context"
	"global"
	"hlog"
	"homectl/options"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.Flags().BoolVarP(&disableDeviceManager, "disable-device-manager", "D", false, "Disable the device manager")
	Cmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "Force run in foreground (default is automatic)")
	Cmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", 5*time.Second, "Timeout for MQTT operations")
}

var disableDeviceManager bool

var foreground bool

var Cmd = &cobra.Command{
	Use:   "daemon",
	Short: "MyHome Daemon",
	Long:  "MyHome Daemon, with embedded MQTT broker and persistent device manager",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		hlog.Logger.Info("Running daemon")
		mhs := NewService(cmd.Context(), cmd.Context().Value(global.CancelKey).(context.CancelFunc), run)
		return mhs.Run(foreground)
	},
}
