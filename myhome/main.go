package main

import (
	"devices/shelly"
	"devices/shelly/gen1"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hlog"

	"myhome/http"
	"myhome/logs"
	"myhome/mqtt"

	"github.com/asnowfix/home-automation/myhome/devices"
	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
	"github.com/spf13/viper"
)

var Program string
var Repo string
var Version string
var Commit string

func main() {
	log := hlog.Init()

	// Initialize viper
	viper.SetConfigName("myhome") // name of config file (without extension)
	viper.SetConfigType("yaml")   // or viper.SetConfigType("toml")
	viper.AddConfigPath(".")      // optionally look for config in the working directory
	err := viper.ReadInConfig()   // Find and read the config file
	if err != nil {
		log.Error(err, "Error reading config file")
		os.Exit(1)
	}

	// Read the configuration option to disable MQTT server startup
	disableMQTT := viper.GetBool("disable_mqtt")

	// Create signals channel to run server until interrupted
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

	// Initialize DeviceManager
	storage, err := devices.NewDeviceStorage(log, "myhome.db")
	if err != nil {
		log.Error(err, "Failed to initialize device storage")
		os.Exit(1)
	}
	dm := devices.NewDeviceManager(log, storage)
	log.Info("Started device manager", "manager", dm)

	// Conditionally start the MQTT server
	if !disableMQTT {
		mdnsServer, broker, err := mqtt.MyHome(log, "myhome", nil)
		if err != nil {
			log.Error(err, "Error starting MQTT server")
			os.Exit(1)
		}
		log.Info("Started MQTT server & published it over mDNS/Zeroconf", "server", mdnsServer)
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
	defer dm.StopDiscovery()

	// Run server until interrupted
	<-done
	log.Info("Shutting down")
}
