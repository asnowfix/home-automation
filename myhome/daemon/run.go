package daemon

import (
	"context"
	"homectl/options"
	"myhome/devices/impl"
	"myhome/mqtt"
	"myhome/storage"
	"mymqtt"
	"mynet"
	"pkg/shelly"

	"myhome"

	"github.com/go-logr/logr"
)

func run(ctx context.Context, log logr.Logger) error {
	var disableEmbeddedMqttBroker bool = len(options.Flags.MqttBroker) != 0
	var err error

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
	shelly.Init(ctx, options.Flags.MqttTimeout)

	var mc *mymqtt.Client

	resolver := mynet.MyResolver(log.WithName("mynet.Resolver"))
	// defer resolver.Stop()

	// Conditionally start the embedded MQTT broker
	if !disableEmbeddedMqttBroker {
		err := mqtt.MyHome(ctx, log, resolver, "myhome", nil)
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
		mc, err = mymqtt.InitClientE(ctx, log, resolver, options.Flags.MqttBroker, options.Flags.MqttTimeout, options.Flags.MqttGrace, options.Flags.MdnsTimeout)
		if err != nil {
			log.Error(err, "Failed to initialize MQTT client")
			return err
		}
		defer mc.Close()
	}

	resolver.Start(ctx)

	if !disableDeviceManager {
		// Initialize DeviceManager
		storage, err := storage.NewDeviceStorage(log, "myhome.db")
		if err != nil {
			log.Error(err, "Failed to initialize device storage")
			return err
		}
		defer storage.Close()

		dm := impl.NewDeviceManager(ctx, storage, resolver, mc)
		err = dm.Start(ctx)
		if err != nil {
			log.Error(err, "Failed to start device manager")
			return err
		}
		// defer dm.Stop()

		log.Info("Started device manager", "manager", dm)

		_, err = myhome.NewServerE(ctx, log, mc, dm)
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
		<-ctx.Done()
		close(done)
	}()

	// Wait for context cancellation
	<-done
	log.Info("Shutting down")
	return nil
}
