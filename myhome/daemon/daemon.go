package daemon

import (
	"context"
	"fmt"
	"global"
	"myhome/ctl/options"
	"myhome/devices/impl"
	mqttclient "myhome/mqtt"
	mqttserver "myhome/mqtt"
	mynet "myhome/net"
	"myhome/occupancy"
	"myhome/storage"
	"myhome/temperature"
	"myhome/ui"
	"net/http"
	_ "net/http/pprof"
	"pkg/shelly"
	"pkg/shelly/gen1"
	"time"

	"myhome"

	"github.com/asnowfix/home-automation/myhome/metrics"
	"github.com/go-logr/logr"
	"github.com/kardianos/service"
)

type daemon struct {
	ctx              context.Context
	cancel           context.CancelFunc
	dm               *impl.DeviceManager
	rpc              myhome.Server
	occupancyService *occupancy.Service
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

	// Set the instance name for RPC topics
	if options.Flags.InstanceName != "" {
		myhome.InstanceName = options.Flags.InstanceName
	}
	log.Info("Starting MyHome daemon", "instance", myhome.InstanceName)

	// Start pprof HTTP server for profiling
	go func() {
		log.Info("Starting pprof server on :6060")
		if err := http.ListenAndServe(":6060", nil); err != nil {
			log.Error(err, "pprof server failed")
		}
	}()

	var disableEmbeddedMqttBroker bool = len(options.Flags.MqttBroker) != 0

	resolver := mynet.MyResolver(log.WithName("mynet.Resolver"))

	// Conditionally start the embedded MQTT broker
	var mqttBrokerAddr string
	if !disableEmbeddedMqttBroker {
		log.Info("Starting embedded MQTT broker")
		err := mqttserver.Broker(d.ctx, log.WithName("mqtt.Broker"), resolver, "myhome", nil, options.Flags.MqttBrokerClientLogInterval, options.Flags.NoMdnsPublish, options.ViperConfig)
		if err != nil {
			log.Error(err, "Failed to initialize MyHome")
			return err
		}
		// Wait for broker to be ready to accept connections
		if err := mqttserver.WaitForBrokerReady(d.ctx, log.WithName("mqtt.Broker"), 5*time.Second); err != nil {
			log.Error(err, "MQTT broker failed to become ready")
			return err
		}
		// Connect to localhost when using embedded broker
		mqttBrokerAddr = "localhost"
	} else {
		log.Info("Embedded MQTT broker disabled")
		mqttBrokerAddr = options.Flags.MqttBroker
	}

	// Connect to the network's MQTT broker or use the embedded broker
	err = mqttclient.NewClientE(d.ctx, mqttBrokerAddr, options.Flags.MdnsTimeout, options.Flags.MqttTimeout, options.Flags.MqttGrace)
	if err != nil {
		log.Error(err, "Failed to initialize MQTT client")
		return err
	}

	// Start the MQTT client and get the client instance
	mc, err := mqttclient.GetClientE(d.ctx)
	if err != nil {
		log.Error(err, "Failed to start MQTT client")
		return err
	}
	defer mc.Close()

	shelly.Init(log, mc, options.Flags.MqttTimeout, options.Flags.ShellyRateLimit)

	// Start the main HTTP server (as a Mux), given to every other servers started below
	// mux := http.NewServeMux()

	// Start Gen1 (HTTP->MQTT) proxy (auto-enabled with embedded MQTT broker)
	if options.Flags.EnableGen1Proxy {
		log.Info("Starting Gen1 (HTTP->MQTT) proxy")
		gen1.StartHttp2MqttProxy(logr.NewContext(d.ctx, log.WithName("gen1")), 8888, mc)
	} else {
		log.Info("Gen1 (HTTP->MQTT) proxy disabled")
	}

	// Start Occupancy service (MQTT only)
	if options.Flags.EnableOccupancyService {
		log.Info("Starting occupancy service")

		// Create occupancy service
		d.occupancyService = occupancy.NewService(
			d.ctx,
			log.WithName("occupancy"),
			mc,
			12*time.Hour,
			5*time.Minute,
			[]string{"iPhone"},
		)

		// Start Occupancy service
		if err := d.occupancyService.Start(); err != nil {
			log.Error(err, "Failed to start occupancy HTTP service")
			return err
		}

		log.Info("Occupancy service started (HTTP on :8889, RPC will be registered with device manager)")
	} else {
		log.Info("Occupancy service disabled")
	}

	// Start Prometheus Metrics Exporter (auto-enabled with device manager)
	if options.Flags.EnableMetricsExporter {
		log.Info("Starting Prometheus metrics exporter")

		httpAddr := fmt.Sprintf(":%d", options.Flags.MetricsExporterPort)
		exporter := metrics.NewExporter(
			d.ctx,
			log.WithName("metrics"),
			mc,
			options.Flags.MetricsExporterTopic,
			httpAddr,
		)

		if err := exporter.Start(); err != nil {
			log.Error(err, "Failed to start metrics exporter")
			return err
		}

		log.Info("Prometheus metrics exporter started",
			"http_addr", httpAddr,
			"mqtt_topic", options.Flags.MetricsExporterTopic)
	} else {
		log.Info("Prometheus metrics exporter disabled")
	}

	if !disableDeviceManager {
		log.Info("Starting device manager")
		storage, err := storage.NewDeviceStorage(log, "myhome.db")
		if err != nil {
			log.Error(err, "Failed to initialize device storage")
			return err
		}
		defer storage.Close()

		// Create SSE broadcaster for live sensor updates
		sseBroadcaster := ui.NewSSEBroadcaster(log.WithName("sse"))

		// Start device manager
		d.dm = impl.NewDeviceManager(d.ctx, storage, resolver, mc, sseBroadcaster)
		err = d.dm.Start(d.ctx)
		if err != nil {
			log.Error(err, "Failed to start device manager")
			return err
		}
		log.Info("Started device manager", "manager", d.dm)

		// Start the main RPC server
		d.rpc, err = myhome.NewServerE(d.ctx, d.dm)
		if err != nil {
			log.Error(err, "Failed to start MyHome RPC service")
			return err
		}

		// Register Temperature RPC methods if enabled
		if options.Flags.EnableTemperatureService {
			log.Info("Initializing temperature RPC methods")

			// Create temperature storage using the same database
			tempStorage, err := temperature.NewStorage(log, storage.DB())
			if err != nil {
				log.Error(err, "Failed to initialize temperature storage")
				return err
			}

			// Create and register temperature method handlers, republishing temperature ranges at startup
			tempHandlers := temperature.NewService(d.ctx, log, mc, tempStorage)
			tempHandlers.RegisterHandlers()

			log.Info("Temperature RPC methods registered")
		}

		// Register Occupancy RPC methods if enabled
		if options.Flags.EnableOccupancyService && d.occupancyService != nil {
			log.Info("Registering occupancy RPC methods")

			// Create and register occupancy RPC handler
			occupancyHandler := occupancy.NewRPCHandler(log, d.occupancyService)
			occupancyHandler.RegisterHandlers()

			log.Info("Occupancy RPC methods registered")
		}

		// Publish a hostname for the DeviceManager host: myhome.local
		if !options.Flags.NoMdnsPublish {
			resolver.WithLocalName(d.ctx, myhome.MYHOME_HOSTNAME)
		} else {
			log.Info("Skipping mDNS hostname publishing (--no-mdns-publish)")
		}

		// Start UI & reverse HTTP proxy
		if err := ui.Start(d.ctx, log.WithName("server"), options.Flags.UiPort, resolver, storage, mc, sseBroadcaster); err != nil {
			log.Error(err, "Failed to start UI server")
			return err
		}

	} else {
		log.Info("Device manager disabled")
	}

	log.Info("Running")

	// Start periodic MQTT connection status monitoring if enabled
	if options.Flags.MqttBrokerClientLogInterval > 0 {
		go func(ctx context.Context) {
			log := logr.FromContextOrDiscard(ctx)
			ticker := time.NewTicker(options.Flags.MqttBrokerClientLogInterval)
			defer ticker.Stop()

			log.Info("Starting MQTT connection status monitoring", "interval", options.Flags.MqttBrokerClientLogInterval)

			for {
				select {
				case <-ctx.Done():
					log.Info("Stopping MQTT connection status monitoring")
					return
				case <-ticker.C:
					// Log MQTT connection status
					if mc != nil {
						connected := mc.IsConnected()
						log.Info("MQTT client connection status", "connected", connected, "client_id", mc.Id())
					}
				}
			}
		}(logr.NewContext(d.ctx, log.WithName("Mqtt.ClientMonitor")))
	} else {
		log.Info("MQTT connection status monitoring disabled")
	}

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
