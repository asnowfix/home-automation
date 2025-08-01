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
	"time"

	"myhome"

	"github.com/go-logr/logr"
	"github.com/kardianos/service"
)

type daemon struct {
	ctx    context.Context
	cancel context.CancelFunc
	log    logr.Logger
	dm     *impl.DeviceManager
}

type Config struct {
	RefreshInterval time.Duration `json:"refresh_interval"`
}

var DefaultConfig = Config{
	RefreshInterval: 3 * time.Minute,
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

	// Initialize Shelly devices handler
	shelly.Init(d.log, options.Flags.MqttTimeout)

	var mc *mymqtt.Client

	resolver := mynet.MyResolver(d.log.WithName("mynet.Resolver"))

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
		err := mymqtt.NewClientE(d.ctx, d.log, options.Flags.MqttBroker, options.Flags.MdnsTimeout, options.Flags.MqttTimeout, options.Flags.MqttGrace)
		if err != nil {
			d.log.Error(err, "Failed to initialize MQTT client")
			return err
		}
		mc, err = mymqtt.GetClientE(d.ctx)
		if err != nil {
			d.log.Error(err, "Failed to start MQTT client")
			return err
		}
		defer mc.Close()
	}

	if !disableDeviceManager {
		// Initialize DeviceManager
		storage, err := storage.NewDeviceStorage(d.log, "myhome.db")
		if err != nil {
			d.log.Error(err, "Failed to initialize device storage")
			return err
		}
		defer storage.Close()

		d.dm = impl.NewDeviceManager(d.ctx, storage, resolver, mc)
		err = d.dm.Start(d.ctx)
		if err != nil {
			d.log.Error(err, "Failed to start device manager")
			return err
		}

		d.log.Info("Started device manager", "manager", d.dm)

		_, err = myhome.NewServerE(d.ctx, d.log, d.dm)
		if err != nil {
			d.log.Error(err, "Failed to start MyHome service")
			return err
		}
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
