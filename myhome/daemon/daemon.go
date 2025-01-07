package daemon

import (
	"devices/shelly"
	"devices/shelly/gen1"
	"hlog"
	"myhome/http"
	"myhome/logs"
	"myhome/mqtt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/asnowfix/home-automation/myhome/devices"
	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	var disableEmbeddedMqttBroker bool
	var disableDeviceManager bool

	log := hlog.Init()

	// Initialize viper
	viper.SetConfigName("myhome") // name of config file (without extension)
	viper.SetConfigType("yaml")   // or viper.SetConfigType("toml")
	viper.AddConfigPath(".")      // optionally look for config in the working directory
	err := viper.ReadInConfig()   // Find and read the config file
	if err != nil {
		log.Error(err, "Error reading config file")
		disableEmbeddedMqttBroker = false
		disableDeviceManager = true
	} else {
		// Read the configuration option to disable MQTT broker startup
		disableEmbeddedMqttBroker = viper.GetBool("disable_embedded_mqtt")
		disableDeviceManager = viper.GetBool("disable_device_manager")
	}

	// Create signals channel to run server until interrupted
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

	// Conditionally start the embedded MQTT broker
	if !disableEmbeddedMqttBroker {
		mdnsServer, broker, err := mqtt.MyHome(log, "myhome", nil)
		if err != nil {
			log.Error(err, "Error starting MQTT server")
			os.Exit(1)
		}
		log.Info("Started embedded MQTT broker & published it over mDNS/Zeroconf", "server", mdnsServer)
		defer mdnsServer.Shutdown()

		topicsCh := make(chan string, 1)
		defer close(topicsCh)
		go logs.Waiter(log, broker, topicsCh)

		gen1Ch := make(chan gen1.Device, 1)
		go http.MyHome(log, gen1Ch)
		go gen1.Publisher(log, gen1Ch, topicsCh, broker)

		proxyCh := make(chan struct{}, 1)
		go mqtt.CommandProxy(log, proxyCh)
		defer close(proxyCh)
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

		// Initialize Shelly devices
		shelly.Init(log)
		dm.DiscoverDevices(shelly.MDNS_SHELLIES, 5*time.Second, func(log logr.Logger, entry *zeroconf.ServiceEntry) (*devices.DeviceIdentifier, error) {
			log.Info("Identifying", "entry", entry)
			return &devices.DeviceIdentifier{
				Manufacturer: "Shelly",
				ID:           entry.Instance,
			}, nil
		}, func(log logr.Logger, entry *zeroconf.ServiceEntry) (*devices.Device, error) {
			sd, err := shelly.NewDeviceFromZeroConfEntry(log, entry)
			if err != nil {
				return nil, err
			}
			log.Info("Got", "shelly_device", sd)
			return &devices.Device{
				DeviceIdentifier: devices.DeviceIdentifier{
					Manufacturer: "Shelly",
					ID:           sd.Id_,
				},
				MAC:  sd.MacAddress,
				Host: sd.Ipv4_.String(),
			}, nil
		})
	}

	// Run server until interrupted
	<-done
	log.Info("Shutting down")
}
