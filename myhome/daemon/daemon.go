package daemon

import (
	"hlog"
	"homectl/options"
	"myhome/devices/impl"
	"myhome/mqtt"
	"myhome/storage"
	"mymqtt"
	"mynet"
	"pkg/shelly"
	"time"

	"myhome"

	"github.com/kardianos/service"
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
		if service.Interactive() {
			// Running as a normal process
			return runDaemon(cmd, args)
		} else {
			// Running as a Windows Service
			app := &App{
				cmd: cmd,
			}
			return runAsService(app)
		}
	},
}

func runDaemon(cmd *cobra.Command, args []string) error {
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
	shelly.Init(cmd.Context(), options.Flags.MqttTimeout)

	var mc *mymqtt.Client

	resolver := mynet.MyResolver(log.WithName("mynet.Resolver"))
	// defer resolver.Stop()

	// Conditionally start the embedded MQTT broker
	if !disableEmbeddedMqttBroker {
		err := mqtt.MyHome(cmd.Context(), log, resolver, "myhome", nil)
		if err != nil {
			log.Error(err, "Failed to initialize MyHome")
			return err
		}

		// // Connect to the embedded MQTT broker
		// mc, err = mymqtt.InitClientE(ctx, log, resolver, "me", myhome.MYHOME, options.Flags.MqttTimeout)
		// if err != nil {
		// 	log.Error(err, "Failed to initialize MQTT client")
		// 	return err
		// }

		// gen1Ch := make(chan gen1.Device, 1)
		// go http.MyHome(log, gen1Ch)
		// go gen1.Publisher(ctx, log, gen1Ch, mc)
	} else {
		// Connect to the network's MQTT broker
		mc, err = mymqtt.InitClientE(cmd.Context(), log, resolver, options.Flags.MqttBroker, options.Flags.MqttTimeout, options.Flags.MqttGrace, options.Flags.MdnsTimeout)
		if err != nil {
			log.Error(err, "Failed to initialize MQTT client")
			return err
		}
		defer mc.Close()
	}

	resolver.Start(cmd.Context())

	if !disableDeviceManager {
		// Initialize DeviceManager
		storage, err := storage.NewDeviceStorage(log, "myhome.db")
		if err != nil {
			log.Error(err, "Failed to initialize device storage")
			return err
		}
		defer storage.Close()

		dm := impl.NewDeviceManager(cmd.Context(), storage, resolver, mc)
		err = dm.Start(cmd.Context())
		if err != nil {
			log.Error(err, "Failed to start device manager")
			return err
		}
		// defer dm.Stop()

		log.Info("Started device manager", "manager", dm)

		_, err = myhome.NewServerE(cmd.Context(), log, mc, dm)
		if err != nil {
			log.Error(err, "Failed to start MyHome service")
			return err
		}
		// defer server.Close()
	}

	log.Info("Running")

	// Create a channel to handle OS signals
	done := make(chan struct{})
	go func() {
		<-cmd.Context().Done()
		close(done)
	}()

	// Wait for context cancellation
	<-done
	log.Info("Shutting down")
	return nil
}
