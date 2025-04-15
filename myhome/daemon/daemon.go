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

type d struct {
	ctx    context.Context
	cancel context.CancelFunc
	log    logr.Logger
}

func NewDaemon(ctx context.Context, cancel context.CancelFunc, log logr.Logger) *d {
	return &d{
		ctx:    ctx,
		cancel: cancel,
		log:    log,
	}
}

func (d *d) Start(s service.Service) error {
	// Start should not block. Do the actual work async.
	go d.Run()
	return nil
}

func (d *d) Stop(s service.Service) error {
	d.cancel()
	return nil
}

func (p *d) Run() error {
	var disableEmbeddedMqttBroker bool = len(options.Flags.MqttBroker) != 0
	var err error

	// Initialize Shelly devices handler
	shelly.Init(p.ctx, options.Flags.MqttTimeout)

	var mc *mymqtt.Client

	resolver := mynet.MyResolver(p.log.WithName("mynet.Resolver"))
	// defer resolver.Stop()

	// Conditionally start the embedded MQTT broker
	if !disableEmbeddedMqttBroker {
		err := mqtt.MyHome(p.ctx, p.log, resolver, "myhome", nil)
		if err != nil {
			p.log.Error(err, "Failed to initialize MyHome")
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
		mc, err = mymqtt.InitClientE(p.ctx, p.log, resolver, options.Flags.MqttBroker, options.Flags.MqttTimeout, options.Flags.MqttGrace, options.Flags.MdnsTimeout)
		if err != nil {
			p.log.Error(err, "Failed to initialize MQTT client")
			return err
		}
		defer mc.Close()
	}

	resolver.Start(p.ctx)

	if !disableDeviceManager {
		// Initialize DeviceManager
		storage, err := storage.NewDeviceStorage(p.log, "myhome.db")
		if err != nil {
			p.log.Error(err, "Failed to initialize device storage")
			return err
		}
		defer storage.Close()

		dm := impl.NewDeviceManager(p.ctx, storage, resolver, mc)
		err = dm.Start(p.ctx)
		if err != nil {
			p.log.Error(err, "Failed to start device manager")
			return err
		}
		// defer dm.Stop()

		p.log.Info("Started device manager", "manager", dm)

		_, err = myhome.NewServerE(p.ctx, p.log, mc, dm)
		if err != nil {
			p.log.Error(err, "Failed to start MyHome service")
			return err
		}
		// defer server.Close()
	}

	p.log.Info("Running")

	// Create a channel to handle OS signals
	done := make(chan struct{})
	go func() {
		<-p.ctx.Done()
		close(done)
	}()

	// Wait for context cancellation
	<-done
	p.log.Info("Shutting down")
	return nil
}
