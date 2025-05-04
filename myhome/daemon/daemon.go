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
	"github.com/kardianos/service"
)

type daemon struct {
	ctx    context.Context
	cancel context.CancelFunc
	log    logr.Logger
}

func NewDaemon(ctx context.Context, cancel context.CancelFunc, log logr.Logger) *daemon {
	return &daemon{
		ctx:    ctx,
		cancel: cancel,
		log:    log,
	}
}

func (d *daemon) Start(s service.Service) error {
	// Start should not block. Do the actual work async.
	go d.Run()
	return nil
}

func (d *daemon) Stop(s service.Service) error {
	d.cancel()
	return nil
}

func (d *daemon) Run() error {
	var disableEmbeddedMqttBroker bool = len(options.Flags.MqttBroker) != 0
	var err error

	// Initialize Shelly devices handler
	shelly.Init(d.ctx, options.Flags.MqttTimeout)

	var mc *mymqtt.Client

	resolver := mynet.MyResolver(d.log.WithName("mynet.Resolver"))
	// defer resolver.Stop()

	// Conditionally start the embedded MQTT broker
	if !disableEmbeddedMqttBroker {
		err := mqtt.MyHome(d.ctx, d.log, resolver, "myhome", nil)
		if err != nil {
			d.log.Error(err, "Failed to initialize MyHome")
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
		mc, err = mymqtt.InitClientE(d.ctx, d.log, resolver, options.Flags.MqttBroker, options.Flags.MqttTimeout, options.Flags.MqttGrace, options.Flags.MdnsTimeout)
		if err != nil {
			d.log.Error(err, "Failed to initialize MQTT client")
			return err
		}
		defer mc.Close()
	}

	resolver.Start(d.ctx)

	if !disableDeviceManager {
		// Initialize DeviceManager
		storage, err := storage.NewDeviceStorage(d.log, "myhome.db")
		if err != nil {
			d.log.Error(err, "Failed to initialize device storage")
			return err
		}
		defer storage.Close()

		dm := impl.NewDeviceManager(d.ctx, storage, resolver, mc)
		err = dm.Start(d.ctx)
		if err != nil {
			d.log.Error(err, "Failed to start device manager")
			return err
		}
		// defer dm.Stop()

		d.log.Info("Started device manager", "manager", dm)

		_, err = myhome.NewServerE(d.ctx, d.log, mc, dm)
		if err != nil {
			d.log.Error(err, "Failed to start MyHome service")
			return err
		}
		// defer server.Close()
	}

	d.log.Info("Running")

	// Create a channel to handle OS signals
	done := make(chan struct{})
	go func() {
		<-d.ctx.Done()
		close(done)
	}()

	// Wait for context cancellation
	<-done
	d.log.Info("Shutting down")
	return nil
}
