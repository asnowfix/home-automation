package daemon

import (
	"context"
	"global"
	"myhome/ctl/options"
	"myhome/devices/impl"
	mqttclient "myhome/mqtt"
	mqttserver "myhome/mqtt"
	"myhome/occupancy"
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
	dm     *impl.DeviceManager
}

type Config struct {
	RefreshInterval time.Duration `json:"refresh_interval"`
}

var DefaultConfig = Config{
	RefreshInterval: 3 * time.Minute,
}

func NewDaemon(ctx context.Context) *daemon {
	return &daemon{
		ctx:    ctx,
		cancel: ctx.Value(global.CancelKey).(context.CancelFunc),
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
	log, err := logr.FromContext(d.ctx)
	if err != nil {
		return err
	}
	log.Info("Starting MyHome daemon")

	var disableEmbeddedMqttBroker bool = len(options.Flags.MqttBroker) != 0

	// Initialize Shelly devices handler
	shelly.Init(log, options.Flags.MqttTimeout)

	var mc *mqttclient.Client

	resolver := mynet.MyResolver(log.WithName("mynet.Resolver"))

	// Conditionally start the embedded MQTT broker
	if !disableEmbeddedMqttBroker {
		log.Info("Starting embedded MQTT broker")
		err := mqttserver.Broker(d.ctx, log.WithName("mqtt.Broker"), resolver, "myhome", nil)
		if err != nil {
			log.Error(err, "Failed to initialize MyHome")
			return err
		}
	} else {
		log.Info("Embedded MQTT broker disabled")
	}

	// Connect to the network's MQTT broker or use the embedded broker
	err = mqttclient.NewClientE(d.ctx, log.WithName("mqttclient.Client"), options.Flags.MqttBroker, options.Flags.MdnsTimeout, options.Flags.MqttTimeout, options.Flags.MqttGrace)
	if err != nil {
		log.Error(err, "Failed to initialize MQTT client")
		return err
	}
	mc, err = mqttclient.GetClientE(d.ctx)
	if err != nil {
		log.Error(err, "Failed to start MQTT client")
		return err
	}
	defer mc.Close()

	// Start Gen1 (HTTP->MQTT) proxy only when explicitly enabled, or with embedded MQTT broker
	if options.Flags.EnableGen1Proxy {
		log.Info("Starting Gen1 (HTTP->MQTT) proxy")
		gen1.StartHttp2MqttProxy(logr.NewContext(d.ctx, log.WithName("gen1")), 8888, mc)
	} else {
		log.Info("Gen1 (HTTP->MQTT) proxy disabled")
	}

	// Start Occupancy HTTP service (follows MQTT broker)
	if options.Flags.EnableOccupancyService {
		log.Info("Starting occupancy HTTP service")
		if err := occupancy.Start(logr.NewContext(d.ctx, log.WithName("occupancy")), 8889, mc); err != nil {
			log.Error(err, "Failed to start occupancy service")
			return err
		}
	} else {
		log.Info("Occupancy HTTP service disabled")
	}

	if !disableDeviceManager {
		log.Info("Starting device manager")
		storage, err := storage.NewDeviceStorage(log, "myhome.db")
		if err != nil {
			log.Error(err, "Failed to initialize device storage")
			return err
		}
		defer storage.Close()

		// Start UI reverse HTTP proxy
		if err := proxy.Start(d.ctx, log.WithName("proxy"), options.Flags.ProxyPort, resolver, storage); err != nil {
			log.Error(err, "Failed to start reverse proxy")
			return err
		}

		d.dm = impl.NewDeviceManager(d.ctx, storage, resolver, mc)
		err = d.dm.Start(d.ctx)
		if err != nil {
			log.Error(err, "Failed to start device manager")
			return err
		}

		log.Info("Started device manager", "manager", d.dm)

		_, err = myhome.NewServerE(d.ctx, log, d.dm)
		if err != nil {
			log.Error(err, "Failed to start MyHome service")
			return err
		}

		// Publish a hostname for the DeviceManager host: myhome.local
		resolver.WithLocalName(d.ctx, myhome.MYHOME_HOSTNAME)
	} else {
		log.Info("Device manager disabled")
	}

	log.Info("Running")

	// Create a channel to handle OS signals
	done := make(chan struct{})
	go func() {
		<-d.ctx.Done()
		close(done)
	}()

	// Wait for context cancellation
	<-done
	log.Info("Shutting down")
	return nil
}
