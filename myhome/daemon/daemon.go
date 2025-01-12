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
	Cmd.Flags().StringVarP(&mqttBroker, "mqtt-broker", "B", "", "Specify the MQTT broker to use, using the format <hostname>:<port>")
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

	// Initialize Shelly devices handler
	shelly.Init(log)

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
		err = dm.Start(mc)
		if err != nil {
			log.Error(err, "Failed to start device manager")
			os.Exit(1)
		}

		defer dm.Stop()
		log.Info("Started device manager", "manager", dm)
	}

	// Run server until interrupted
	<-done
	log.Info("Shutting down")
}
