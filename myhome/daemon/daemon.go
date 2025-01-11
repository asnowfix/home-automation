package daemon

import (
	"hlog"
	"myhome/http"
	"myhome/mqtt"
	"mymqtt"
	"os"
	"os/signal"
	"pkg/shelly"
	"pkg/shelly/gen1"
	"syscall"

	"myhome/devices"

	"github.com/spf13/cobra"
)

// cobra command for the daemon
var (
	mqttBroker string
	Cmd        = &cobra.Command{
		Use:   "daemon",
		Short: "MyHome Daemon",
		Long:  "MyHome Daemon",
		Run: func(cmd *cobra.Command, args []string) {
			Run()
		},
	}
)

func init() {
	// Define the MQTT broker option
	Cmd.Flags().StringVarP(&mqttBroker, "mqtt-broker", "M", "", "Specify the MQTT broker to use, using the format <hostname>:<port>")
}

func Run() {
	var disableEmbeddedMqttBroker bool = len(mqttBroker) != 0
	var disableDeviceManager bool = false
	var err error

	log := hlog.Init()

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

	var mc *mymqtt.Client

	// Conditionally start the embedded MQTT broker
	if !disableEmbeddedMqttBroker {
		mdnsServer, _, err := mqtt.MyHome(log, "myhome", nil)
		if err != nil {
			log.Error(err, "Error starting MQTT server")
			os.Exit(1)
		}
		log.Info("Started embedded MQTT broker & published it over mDNS/Zeroconf", "server", mdnsServer)
		defer mdnsServer.Shutdown()

		// Connect to the embedded MQTT broker
		mc, err = mymqtt.NewClientE(log, "me")
		if err != nil {
			log.Error(err, "Failed to initialize MQTT client")
			os.Exit(1)
		}

		gen1Ch := make(chan gen1.Device, 1)
		go http.MyHome(log, gen1Ch)
		go gen1.Publisher(log, gen1Ch, mc)
	} else {
		// Connect to the network's MQTT broker
		mc, err = mymqtt.NewClientE(log, mqttBroker)
		if err != nil {
			log.Error(err, "Failed to initialize MQTT client")
			os.Exit(1)
		}
	}

	if !disableDeviceManager {
		// Initialize DeviceManager
		storage, err := devices.NewDeviceStorage(log, "myhome.db")
		if err != nil {
			log.Error(err, "Failed to initialize device storage")
			os.Exit(1)
		}
		dm := devices.NewDeviceManager(log, storage)
		log.Info("Started device manager", "manager", dm)
		defer dm.StopDiscovery()

		// Initialize Shelly devices handler
		shelly.Init(log)

		// Loop on MQTT event devices discovery
		dm.WatchMqtt(mc)

		// Loop on ZeroConf devices discovery
		// dm.DiscoverDevices(shelly.MDNS_SHELLIES, 300*time.Second, func(log logr.Logger, entry *zeroconf.ServiceEntry) (*devices.DeviceIdentifier, error) {
		// 	log.Info("Identifying", "entry", entry)
		// 	return &devices.DeviceIdentifier{
		// 		Manufacturer: "Shelly",
		// 		ID:           entry.Instance,
		// 	}, nil
		// }, func(log logr.Logger, entry *zeroconf.ServiceEntry) (*devices.Device, error) {
		// 	sd, err := shelly.NewDeviceFromZeroConfEntry(log, entry)
		// 	if err != nil {
		// 		return nil, err
		// 	}
		// 	log.Info("Got", "shelly_device", sd)
		// 	return &devices.Device{
		// 		DeviceIdentifier: devices.DeviceIdentifier{
		// 			Manufacturer: "Shelly",
		// 			ID:           sd.Id_,
		// 		},
		// 		MAC:  sd.MacAddress,
		// 		Host: sd.Ipv4_.String(),
		// 	}, nil
		// })
	}

	// Run server until interrupted
	<-done
	log.Info("Shutting down")
}
