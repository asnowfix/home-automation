package daemon

import (
	"context"
	"hlog"
	"homectl/options"
	"myhome/http"
	"myhome/mqtt"
	"myhome/storage"
	"mymqtt"
	"os"
	"os/signal"
	"pkg/shelly"
	"pkg/shelly/gen1"
	"syscall"

	"myhome"

	"myhome/devices"

	"github.com/spf13/cobra"
)

// cobra command for the daemon
var (
	disableDeviceManager bool
	mqttBroker           string
	Cmd                  = &cobra.Command{
		Use:   "daemon",
		Short: "MyHome Daemon",
		Long:  "MyHome Daemon, with embedded MQTT broker and persistent device manager",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return Run()
		},
	}
)

func init() {
	Cmd.Flags().BoolVarP(&disableDeviceManager, "disable-device-manager", "D", false, "Disable the device manager")
	Cmd.Flags().StringVarP(&mqttBroker, "mqtt-broker", "B", "", "Specify the MQTT broker to connect to (do not start built-in one), using the format <hostname>:<port>")
}

func Run() error {
	var disableEmbeddedMqttBroker bool = len(mqttBroker) != 0
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

	// Create signals channel to run server until interrupted
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

	// Initialize Shelly devices handler
	shelly.Init(log, options.Flags.MqttTimeout)

	var mc *mymqtt.Client
	ctx := context.Background()

	// Conditionally start the embedded MQTT broker
	if !disableEmbeddedMqttBroker {
		mdnsServer, _, err := mqtt.MyHome(log, "myhome", nil)
		if err != nil {
			log.Error(err, "Failed to initialize mDNS server")
			return err
		}
		log.Info("Started embedded MQTT broker & published it over mDNS/Zeroconf", "server", mdnsServer)
		defer mdnsServer.Shutdown()

		// Connect to the embedded MQTT broker
		mc, err = mymqtt.NewClientE(log, "me", myhome.MYHOME)
		if err != nil {
			log.Error(err, "Failed to initialize MQTT client")
			return err
		}

		gen1Ch := make(chan gen1.Device, 1)
		go http.MyHome(log, gen1Ch)
		go gen1.Publisher(log, gen1Ch, mc)
	} else {
		// Connect to the network's MQTT broker
		mc, err = mymqtt.NewClientE(log, mqttBroker, myhome.MYHOME)
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
		err = dm.Start(ctx)
		if err != nil {
			log.Error(err, "Failed to start device manager")
			return err
		}

		defer dm.Shutdown()
		log.Info("Started device manager", "manager", dm)

		ds, err := myhome.NewServerE(ctx, log, mc, dm)
		if err != nil {
			log.Error(err, "Failed to start MyHome service")
			return err
		}
		defer ds.Shutdown()
	}

	log.Info("Running")
	// Run server until interrupted
	<-done
	mc.Close()
	log.Info("Shutting down")

	return nil
}
