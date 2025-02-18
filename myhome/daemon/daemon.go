package daemon

import (
	"hlog"
	"homectl/options"
	"myhome/mqtt"
	"myhome/storage"
	"mymqtt"
	"pkg/shelly"
	"time"

	"myhome"

	"myhome/devices"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.Flags().BoolVarP(&disableDeviceManager, "disable-device-manager", "D", false, "Disable the device manager")
	Cmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", 5*time.Second, "Timeout for MQTT operations")
}

var disableDeviceManager bool

var Cmd = &cobra.Command{
	Use:   "daemon",
	Short: "MyHome Daemon",
	Long:  "MyHome Daemon, with embedded MQTT broker and persistent device manager",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		var disableEmbeddedMqttBroker bool = len(options.Flags.MqttBroker) != 0
		var err error
		log := hlog.Logger

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

		// Initialize Shelly devices handler
		shelly.Init(log, options.Flags.MqttTimeout)

		var mc *mymqtt.Client

		// Conditionally start the embedded MQTT broker
		if !disableEmbeddedMqttBroker {
			mdnsServer, _, err := mqtt.MyHome(log, "myhome", nil)
			if err != nil {
				log.Error(err, "Failed to initialize mDNS server")
				return err
			}
			log.Info("Started embedded MQTT broker & published it over mDNS/Zeroconf", "server", mdnsServer)
			defer mdnsServer.Shutdown()

			// // Connect to the embedded MQTT broker
			// mc, err = mymqtt.InitClientE(ctx, log, "me", myhome.MYHOME, options.Flags.MqttTimeout)
			// if err != nil {
			// 	log.Error(err, "Failed to initialize MQTT client")
			// 	return err
			// }

			// gen1Ch := make(chan gen1.Device, 1)
			// go http.MyHome(log, gen1Ch)
			// go gen1.Publisher(ctx, log, gen1Ch, mc)
		} else {
			// Connect to the network's MQTT broker
			mc, err = mymqtt.InitClientE(cmd.Context(), log, options.Flags.MqttBroker, options.Flags.MqttTimeout, options.Flags.MqttGrace)
			if err != nil {
				log.Error(err, "Failed to initialize MQTT client")
				return err
			}
		}

		if !disableDeviceManager {
			// Initialize DeviceManager
			storage, err := storage.NewDeviceStorage(log, "myhome.db")
			if err != nil {
				log.Error(err, "Failed to initialize device storage")
				return err
			}

			dm := devices.NewDeviceManager(log, storage, mc)
			err = dm.Start(cmd.Context())
			if err != nil {
				log.Error(err, "Failed to start device manager")
				return err
			}

			defer dm.Shutdown()
			log.Info("Started device manager", "manager", dm)

			ds, err := myhome.NewServerE(cmd.Context(), log, mc, dm)
			if err != nil {
				log.Error(err, "Failed to start MyHome service")
				return err
			}
			defer ds.Shutdown()
		}

		log.Info("Running")
		// Run server until interrupted
		<-cmd.Context().Done()
		log.Info("Shutting down")
		return nil
	},
}
