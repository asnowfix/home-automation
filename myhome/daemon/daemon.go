package daemon

import (
	"context"
	"myhome/ctl/options"
	"myhome/devices/impl"
	mqttclient "myhome/mqtt"
	mqttserver "myhome/mqtt"
	"myhome/proxy"
	"myhome/storage"
	"mynet"
	"pkg/shelly"
	"pkg/shelly/gen1"
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

	var mc *mqttclient.Client

	resolver := mynet.MyResolver(d.log.WithName("mynet.Resolver"))

	// Conditionally start the embedded MQTT broker
	if !disableEmbeddedMqttBroker {
		err := mqttserver.Broker(d.ctx, d.log.WithName("mqtt.Broker"), resolver, "myhome", nil)
		if err != nil {
			d.log.Error(err, "Failed to initialize MyHome")
			return err
		}
	}

	// Connect to the network's MQTT broker or use the embedded broker
	err := mqttclient.NewClientE(d.ctx, d.log.WithName("mqttclient.Client"), options.Flags.MqttBroker, options.Flags.MdnsTimeout, options.Flags.MqttTimeout, options.Flags.MqttGrace)
	if err != nil {
		d.log.Error(err, "Failed to initialize MQTT client")
		return err
	}
	mc, err = mqttclient.GetClientE(d.ctx)
	if err != nil {
		d.log.Error(err, "Failed to start MQTT client")
		return err
	}
	defer mc.Close()

	// Proxy from Gen1 (HTTP-only) devices to MQTT, co-located with the embedded MQTT broker
	if !disableEmbeddedMqttBroker {
		gen1.Proxy(d.ctx, d.log.WithName("gen1.Listen"), 8888, mc)
	}

	if !disableDeviceManager {
		// Initialize DeviceManager
		storage, err := storage.NewDeviceStorage(d.log, "myhome.db")
		if err != nil {
			d.log.Error(err, "Failed to initialize device storage")
			return err
		}
		defer storage.Close()

		// Start UI reverse HTTP proxy
		if err := proxy.Start(d.ctx, d.log.WithName("proxy"), options.Flags.ProxyPort, resolver, storage); err != nil {
			d.log.Error(err, "Failed to start reverse proxy")
			return err
		}

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

		// Publish a hostname for the DeviceManager host: myhome.local
		resolver.WithLocalName(d.ctx, myhome.MYHOME_HOSTNAME)
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
