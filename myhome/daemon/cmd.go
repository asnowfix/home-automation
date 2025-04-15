package daemon

import (
	"context"
	"global"
	"homectl/options"
	"time"

	"github.com/go-logr/logr"
	"github.com/kardianos/service"
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
		// // Initialize viper
		// viper.SetConfigName("myhome") // name of config file (without extension)
		// viper.SetConfigType("yaml")   // or viper.SetConfigType("toml")
		// viper.AddConfigPath(".")      // optionally look for config in the working directory
		// err := viper.ReadInConfig()   // Find and read the config file
		// if err != nil {
		// 	log.Error(err, "Error reading config file")
		// 	disableEmbeddedMqttBroker = false
		// 	disableDeviceManager = true
		// } else {
		// 	// Read the configuration option to disable MQTT broker startup
		// 	disableEmbeddedMqttBroker = viper.GetBool("disable_embedded_mqtt")
		// 	disableDeviceManager = viper.GetBool("disable_device_manager")
		// }

		ctx := cmd.Context()
		cancel := ctx.Value(global.CancelKey).(context.CancelFunc)
		log := ctx.Value(global.LogKey).(logr.Logger)

		config := service.Config{
			Name:        "myhome",
			DisplayName: "MyHome",
			Description: "MyHome Daemon, with embedded MQTT broker and persistent device manager",
		}

		daemon := NewDaemon(ctx, cancel, log)
		if foreground {
			log.Info("Starting service in foreground")
			return daemon.Run()
		} else {
			s, err := service.New(daemon, &config)
			if err != nil {
				log.Error(err, "Failed to create (background) service")
				return err
			}
			logger, err := s.Logger(nil)
			if err != nil {
				log.Error(err, "Failed to get service logger")
				return err
			}
			logger.Info("Starting service")
			err = s.Run()
			if err != nil {
				log.Error(err, "Failed to run service")
				return err
			}
			logger.Info("Service started")
			return nil
		}
	},
}
