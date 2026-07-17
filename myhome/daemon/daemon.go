package daemon

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"time"

	"github.com/asnowfix/home-automation/internal/global"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/internal/myhome/accounts"
	mynet "github.com/asnowfix/home-automation/internal/myhome/net"
	myhomesfr "github.com/asnowfix/home-automation/internal/myhome/sfr"
	shellygen2l "github.com/asnowfix/home-automation/internal/myhome/shelly/gen2"
	"github.com/asnowfix/home-automation/internal/myhome/ui"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/asnowfix/home-automation/myhome/devices/impl"
	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/asnowfix/home-automation/myhome/metrics"
	mqttclient "github.com/asnowfix/home-automation/myhome/mqtt"
	mqttserver "github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/asnowfix/home-automation/myhome/notice"
	"github.com/asnowfix/home-automation/myhome/notify"
	"github.com/asnowfix/home-automation/myhome/occupancy"
	"github.com/asnowfix/home-automation/myhome/storage"
	"github.com/asnowfix/home-automation/myhome/temperature"
	beem "github.com/asnowfix/home-automation/pkg/beem"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/gen1"
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

// reportingMailer wraps a notify.Mailer to report each Send outcome into the
// accounts registry, without notify (a leaf, provider-agnostic package)
// needing to know about account-status tracking.
type reportingMailer struct {
	notify.Mailer
	registry *accounts.Registry
}

func (m *reportingMailer) Send(ctx context.Context, subject, body string) error {
	err := m.Mailer.Send(ctx, subject, body)
	m.registry.Report("smtp", err)
	return err
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

		err := mqttserver.Broker(d.ctx, log, resolver, "myhome", nil, options.Flags.MqttBrokerClientLogInterval, options.Flags.NoMdnsPublish, options.ViperConfig)
		if err != nil {
			log.Error(err, "Failed to initialize MyHome")
			return err
		}
		// Connect to localhost when using embedded broker
		// Include port so GetServer() returns host:port correctly
		mqttBrokerAddr = fmt.Sprintf("localhost:%d", mqttclient.PRIVATE_PORT)

		// Get broker readiness initial delay from config
		readinessInitialDelay := 500 * time.Millisecond // default 500ms

		if options.ViperConfig != nil {
			if options.ViperConfig.IsSet("daemon.broker_readiness_initial_delay") {
				readinessInitialDelay = options.ViperConfig.GetDuration("daemon.broker_readiness_initial_delay")
			}
		}

		// Give embedded broker time to complete async initialization
		// Mochi MQTT's Serve() is non-blocking and listeners start asynchronously
		if readinessInitialDelay > 0 {
			log.Info("Waiting for embedded broker async initialization", "delay", readinessInitialDelay)
			time.Sleep(readinessInitialDelay)
		}
	} else {
		log.Info("Embedded MQTT broker disabled")
		mqttBrokerAddr = options.Flags.MqttBroker
	}

	// Connect to the network's MQTT broker or use the embedded broker
	err = mqttclient.NewClientE(d.ctx, mqttBrokerAddr, myhome.InstanceName, options.Flags.MdnsTimeout, options.Flags.MqttTimeout, options.Flags.MqttGrace, options.Flags.MqttReconnectInterval, false)
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

	// accountsRegistry tracks the connection status of every external
	// account myhome talks to (Beem, SFR, SMTP, MQTT), surfaced in the UI's
	// accounts panel. Each integration below reports into it at its existing
	// connection/poll point; a disabled integration is marked enabled=false
	// so it reads as "not configured" rather than "failed".
	accountsRegistry := accounts.NewRegistry()

	// Start Beem Energy watcher if enabled
	var beemWatcher *beem.Watcher
	beemEnabled := options.Flags.BeemEmail != "" && options.Flags.BeemPassword != ""
	accountsRegistry.SetEnabled("beem", beemEnabled)
	if beemEnabled {
		log.Info("Starting Beem Energy watcher")
		beemCfg := beem.ClientConfig{
			Email:        options.Flags.BeemEmail,
			Password:     options.Flags.BeemPassword,
			PollInterval: options.Flags.BeemPollInterval,
		}
		beemWatcher = beem.NewWatcher(d.ctx, beemCfg, mc)
		beemWatcher.OnResult = func(err error) { accountsRegistry.Report("beem", err) }
		if err := beemWatcher.Start(d.ctx); err != nil {
			log.Error(err, "Failed to start Beem watcher")
			return err
		}
		log.Info("Beem Energy watcher started")
	} else {
		log.Info("Beem Energy integration disabled")
	}

	// SFR box: the device manager (below) starts a periodic refresh loop via
	// myhomesfr.GetRouter regardless of credentials — auth is skipped
	// internally when username/password are empty. Report status from every
	// refresh attempt; SetStatusReporter must be called before the device
	// manager starts (it triggers the first refresh synchronously).
	sfrEnabled := options.Flags.SFRUsername != "" && options.Flags.SFRPassword != ""
	accountsRegistry.SetEnabled("sfr", sfrEnabled)
	myhomesfr.SetStatusReporter(func(err error) { accountsRegistry.Report("sfr", err) })

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

		// Start event service if enabled
		var eventsSvc *events.Service
		var eventsTracker *events.SensorDailyTracker
		var eventsStore *events.Storage

		// noticeSvc and poolNotices are set below (after eventsSvc exists,
		// since both need to call eventsSvc.Record). The broadcast closure
		// below captures the variables themselves, not their values, so it
		// sees the later assignment — by the time any event is actually
		// broadcast, both have long since been wired up.
		var noticeSvc *notice.Service
		var poolNotices *PoolNotices
		broadcastFn := func(e events.Event) {
			sseBroadcaster.BroadcastEvent(e)
			if noticeSvc != nil {
				noticeSvc.OnEvent(d.ctx, e)
			}
			poolNotices.OnEvent(d.ctx, e)
		}

		if options.Flags.EnableEventsService {
			eventsStore, err = events.NewStorage(log.WithName("events"), options.Flags.EventsDBPath)
			if err != nil {
				log.Error(err, "Failed to initialize events storage", "path", options.Flags.EventsDBPath)
				// Non-fatal: continue without event recording
				eventsStore = nil
			} else {
				eventsTracker = events.NewSensorDailyTracker(log.WithName("events"), eventsStore)
				eventsSvc = events.NewService(log.WithName("events"), eventsStore, eventsTracker, broadcastFn, options.Flags.EventsRetention)
				go eventsTracker.Start(d.ctx)
				go eventsSvc.Start(d.ctx)
				log.Info("Events service started", "db", options.Flags.EventsDBPath, "retention", options.Flags.EventsRetention)
			}
		} else {
			log.Info("Events service disabled")
		}

		// Start the notice service (curated "notice"-severity events + daily
		// email digest) if enabled. Requires both the events service (to
		// record derived motion notices and query the digest) and the
		// occupancy service (to tell motion-while-absent from routine
		// motion). Degraded mode: if disabled or its dependencies aren't
		// running, every other notice source (pool/garden plans emitted
		// on-device, solar pump notices) still gets recorded as a plain
		// "notice"-severity event — only the motion rule and the email
		// digest are unavailable.
		if options.Flags.EnableNoticeService {
			if eventsSvc == nil {
				log.Info("Notice service disabled: events service is not running")
			} else if d.occupancyService == nil {
				log.Info("Notice service disabled: occupancy service is not running")
			} else {
				accountsRegistry.SetEnabled("smtp", options.Flags.SMTPFrom != "")
				mailer := &reportingMailer{
					Mailer: notify.New(log.WithName("notify"), notify.Config{
						Host:     options.Flags.SMTPHost,
						Port:     options.Flags.SMTPPort,
						Username: options.Flags.SMTPUsername,
						Password: options.Flags.SMTPPassword,
						From:     options.Flags.SMTPFrom,
						To:       options.Flags.SMTPTo,
					}),
					registry: accountsRegistry,
				}
				noticeSvc = notice.NewService(log.WithName("notice"), eventsSvc, d.occupancyService, mailer, notice.Config{
					NightStart: options.Flags.NoticeNightStart,
					NightEnd:   options.Flags.NoticeNightEnd,
					DigestHour: options.Flags.NoticeDigestHour,
				})
				go noticeSvc.Start(d.ctx)
				log.Info("Notice service started",
					"night_start", options.Flags.NoticeNightStart,
					"night_end", options.Flags.NoticeNightEnd,
					"digest_hour", options.Flags.NoticeDigestHour,
					"email_enabled", options.Flags.SMTPFrom != "",
				)
			}
		} else {
			log.Info("Notice service disabled")
		}

		// Initialize pool runtime tracker if enabled
		var poolTracker *PoolRuntimeTracker
		if options.Flags.PoolEnabled && options.Flags.PoolDeviceID != "" && eventsStore != nil {
			poolTracker = NewPoolRuntimeTracker(log.WithName("pool"), eventsStore, options.Flags.PoolDeviceID)
			log.Info("Pool runtime tracker initialized", "device_id", options.Flags.PoolDeviceID)
		} else if options.Flags.PoolEnabled {
			log.Info("Pool runtime tracker disabled (no device ID or events store unavailable)")
		}

		// Record a "pool.turnover_today" notice whenever the pump stops
		// (schedule/manual pool.pump_stop, or solar-driven pool.solar_stop).
		// Degraded mode: if the pool device can't be reached, NewPoolNotices
		// returns nil and broadcastFn's poolNotices.OnEvent call is then a
		// no-op — pump control itself is entirely unaffected either way.
		if poolTracker != nil {
			poolNotices = NewPoolNotices(d.ctx, log.WithName("pool"), eventsSvc, poolTracker, options.Flags.PoolDeviceID)
		}

		// Start solar automation if enabled
		if options.Flags.PoolSolarEnabled && options.Flags.PoolDeviceID != "" && beemWatcher != nil {
			if options.Flags.PoolSolarMaxVolumeTurnover < options.Flags.PoolSolarMinVolumeTurnover {
				log.Error(nil, "Solar automation disabled: pool.solar.max_volume_turnover must be >= min_volume_turnover",
					"min_volume_turnover", options.Flags.PoolSolarMinVolumeTurnover,
					"max_volume_turnover", options.Flags.PoolSolarMaxVolumeTurnover,
				)
			} else if pumpCtrl, err := newShellyPumpController(d.ctx, log.WithName("solar.pump"), options.Flags.PoolDeviceID); err != nil {
				log.Error(err, "Failed to create pump controller for solar automation")
			} else if dailyTargetSec, maxRotationSec, err := computeRuntimeTargets(
				d.ctx, log.WithName("solar"), pumpCtrl.device,
				options.Flags.PoolSolarMinVolumeTurnover, options.Flags.PoolSolarMaxVolumeTurnover,
			); err != nil {
				log.Error(err, "Solar automation disabled: failed to derive runtime targets from pool KVS")
			} else {
				solarCfg := SolarConfig{
					StartThresholdW: options.Flags.PoolSolarStartThresholdW,
					StopThresholdW:  options.Flags.PoolSolarStopThresholdW,
					StartDelay:      options.Flags.PoolSolarStartDelay,
					StopDelay:       options.Flags.PoolSolarStopDelay,
					DailyTargetSec:  dailyTargetSec,
					MaxRotationSec:  maxRotationSec,
				}
				solarAuto := NewSolarAutomation(
					log.WithName("solar"),
					beemWatcher.PowerCh,
					poolTracker, // nil if pool tracker not enabled
					pumpCtrl,
					solarCfg,
				)
				// eventsSvc may be nil (events service disabled) — WithEvents
				// degrades to a no-op in that case; solar pump control is
				// unaffected either way (see SolarAutomation.recordNotice).
				solarAuto.WithEvents(eventsSvc, options.Flags.PoolDeviceID)
				solarAuto.Start(d.ctx)
				log.Info("Solar automation started",
					"device_id", options.Flags.PoolDeviceID,
					"start_threshold_w", solarCfg.StartThresholdW,
					"stop_threshold_w", solarCfg.StopThresholdW,
					"start_delay", solarCfg.StartDelay,
					"stop_delay", solarCfg.StopDelay,
					"daily_target_sec", solarCfg.DailyTargetSec,
					"max_rotation_sec", solarCfg.MaxRotationSec,
				)
			}
		} else if options.Flags.PoolSolarEnabled {
			if options.Flags.PoolDeviceID == "" {
				log.Info("Solar automation disabled: no pool device ID configured")
			} else {
				log.Info("Solar automation disabled: Beem Energy watcher not running")
			}
		}

		// Start device manager
		d.dm = impl.NewDeviceManager(d.ctx, storage, resolver, mc, sseBroadcaster)
		d.dm.WithEventService(eventsSvc, eventsTracker)
		err = d.dm.Start(d.ctx)
		if err != nil {
			log.Error(err, "Failed to start device manager")
			return err
		}
		log.Info("Started device manager", "manager", d.dm)

		// Start Gen2 NotifyEvent listener
		if eventsSvc != nil && eventsTracker != nil {
			gen2Listener := shellygen2l.NewListener(log.WithName("gen2"), mc, eventsSvc, eventsTracker)
			go gen2Listener.Start(d.ctx)
			log.Info("Gen2 event listener started")
		}

		// Start the main RPC server
		d.rpc, err = myhome.NewServerE(d.ctx, mc, d.dm)
		if err != nil {
			log.Error(err, "Failed to start MyHome RPC service")
			return err
		}

		// Register EventList RPC handler if events service is running
		if eventsStore != nil {
			myhome.RegisterMethodHandler(myhome.EventList, func(ctx context.Context, in any) (any, error) {
				req, ok := in.(*myhome.EventListRequest)
				if !ok {
					return nil, fmt.Errorf("unexpected param type: %T", in)
				}
				q := events.Query{
					DeviceID:  req.DeviceID,
					EventType: req.EventType,
					Severity:  req.Severity,
					Since:     req.Since,
					Limit:     req.Limit,
					Offset:    req.Offset,
				}
				rows, err := eventsStore.Query(ctx, q)
				if err != nil {
					return nil, err
				}
				views := make([]myhome.EventView, len(rows))
				for i, e := range rows {
					views[i] = myhome.EventView{
						ID:         e.ID,
						Ts:         e.Ts,
						ReceivedAt: e.ReceivedAt,
						DeviceID:   e.DeviceID,
						Component:  e.Component,
						Event:      e.Event,
						Severity:   e.Severity,
						Data:       e.Data,
					}
				}
				return &myhome.EventListResponse{Events: views, Total: len(views)}, nil
			})
			log.Info("EventList RPC handler registered")
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

		// Register Pool RPC methods (turnover rate + water-supply status for
		// the UI and `ctl pool status`). Always registered — the signature
		// exists regardless of pool tracking; handleGetStatus itself returns
		// a clear error if poolNotices is nil (pool disabled/unreachable).
		poolRPCHandler := NewPoolRPCHandler(log, poolNotices)
		poolRPCHandler.RegisterHandlers()

		// Publish a hostname for the DeviceManager host: myhome.local
		if !options.Flags.NoMdnsPublish {
			resolver.WithLocalName(d.ctx, myhome.MYHOME_HOSTNAME)
		} else {
			log.Info("Skipping mDNS hostname publishing (--no-mdns-publish)")
		}

		// Explicitly connect MQTT client to trigger OnConnect callback
		// This processes all pending subscriptions before HTTP server starts
		log.Info("Connecting MQTT client (all subscriptions queued)")
		if err := mc.Start(); err != nil {
			log.Error(err, "Failed to connect MQTT client")
			return err
		}
		log.Info("MQTT client connected - subscriptions active")
		accountsRegistry.SetEnabled("mqtt", true)
		accountsRegistry.Report("mqtt", nil)

		// Subscribe to $SYS topics for monitoring (after connection to avoid early connection)
		// Use large buffer (256) because broker publishes 15-20+ $SYS topics in rapid bursts every 30s
		log.Info("Subscribing to $SYS/clients/# topics for broker monitoring")
		err = mc.SubscribeWithHandler(d.ctx, "$SYS/clients/#", 256, "MqttClient/sys/clients", func(topic string, payload []byte, subscriber string) error {
			// Use V(1) to reduce log volume - $SYS topics are published frequently
			log.V(1).Info("Received on $SYS/clients/#", "topic", topic, "payload", string(payload))
			return nil
		})
		if err != nil {
			log.Error(err, "Failed to subscribe to $SYS topics")
			// Don't fail initialization - $SYS subscription is optional
		} else {
			log.Info("Successfully subscribed to $SYS topics")
		}

		// Start UI & reverse HTTP proxy
		// Pass the live device manager (not raw storage) so the dashboard sees
		// each device's in-memory Impl/Status rather than a DB snapshot with no
		// live state attached.
		if err := ui.Start(d.ctx, log.WithName("server"), options.Flags.UiPort, resolver, d.dm, mc, sseBroadcaster, eventsSvc, options.Flags.RemoteProxy, accountsRegistry); err != nil {
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
						var reportErr error
						if !connected {
							reportErr = fmt.Errorf("mqtt client disconnected")
						}
						accountsRegistry.Report("mqtt", reportErr)
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
