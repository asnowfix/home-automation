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
	"github.com/asnowfix/home-automation/internal/myhome/energy"
	mynet "github.com/asnowfix/home-automation/internal/myhome/net"
	myhomesfr "github.com/asnowfix/home-automation/internal/myhome/sfr"
	shellygen2l "github.com/asnowfix/home-automation/internal/myhome/shelly/gen2"
	"github.com/asnowfix/home-automation/internal/myhome/ui"
	"github.com/asnowfix/home-automation/internal/shelly/scripts"
	"github.com/asnowfix/home-automation/myhome/devices/impl"
	"github.com/asnowfix/home-automation/myhome/events"
	"github.com/asnowfix/home-automation/myhome/fetchproxy"
	"github.com/asnowfix/home-automation/myhome/metrics"
	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/asnowfix/home-automation/myhome/notice"
	"github.com/asnowfix/home-automation/myhome/notify"
	"github.com/asnowfix/home-automation/myhome/occupancy"
	"github.com/asnowfix/home-automation/myhome/storage"
	"github.com/asnowfix/home-automation/myhome/temperature"
	beem "github.com/asnowfix/home-automation/pkg/beem"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/gen1"
	shellymqtt "github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/go-logr/logr"
	"github.com/kardianos/service"
)

type daemon struct {
	ctx              context.Context
	cancel           context.CancelFunc
	cfg              Config
	registry         *myhome.Registry
	dm               *impl.DeviceManager
	rpc              myhome.Server
	occupancyService *occupancy.Service
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

// NewDaemon owns its shutdown explicitly: it derives its own cancellable
// context from ctx rather than reaching into ctx for a CancelFunc stashed
// there by an unrelated caller (context values are for request-scoped data,
// not control-flow handles, and a missing/mistyped value would panic here).
// d.cancel (invoked from Stop, on OS service-stop requests) cancels d.ctx;
// d.ctx.Done() also fires if the parent ctx is cancelled (e.g. SIGINT/SIGTERM
// handled upstream in the CLI entry point), so both shutdown paths
// still converge on the same <-d.ctx.Done() wait in Run.
func NewDaemon(ctx context.Context, cfg Config) *daemon {
	ctx, cancel := context.WithCancel(ctx)
	return &daemon{
		ctx:      ctx,
		cancel:   cancel,
		cfg:      cfg,
		registry: myhome.NewRegistry(),
	}
}

func (d *daemon) Start(s service.Service) error {
	// Start should not block. Do the actual work async; Run logs its own
	// errors and the service manager observes liveness, not this return.
	go func() { _ = d.Run() }()
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
	if d.cfg.InstanceName != "" {
		myhome.InstanceName = d.cfg.InstanceName
	}
	log.Info("Starting MyHome daemon", "instance", myhome.InstanceName)

	// Start pprof HTTP server for profiling
	go func() {
		log.Info("Starting pprof server on :6060")
		if err := http.ListenAndServe(":6060", nil); err != nil {
			log.Error(err, "pprof server failed")
		}
	}()

	var disableEmbeddedMqttBroker = len(d.cfg.MqttBroker) != 0

	resolver := mynet.MyResolver(log.WithName("mynet.Resolver"), d.cfg.MdnsTimeout)

	// Conditionally start the embedded MQTT broker
	var mqttBrokerAddr string
	if !disableEmbeddedMqttBroker {
		log.Info("Starting embedded MQTT broker")

		err := mqtt.Broker(d.ctx, log, resolver, "myhome", nil, d.cfg.MqttBrokerClientLogInterval, d.cfg.NoMdnsPublish, d.cfg.Viper)
		if err != nil {
			log.Error(err, "Failed to initialize MyHome")
			return err
		}
		// Connect to localhost when using embedded broker
		// Include port so GetServer() returns host:port correctly
		mqttBrokerAddr = fmt.Sprintf("localhost:%d", mqtt.PRIVATE_PORT)

		// Get broker readiness initial delay from config
		readinessInitialDelay := 500 * time.Millisecond // default 500ms

		if d.cfg.Viper != nil {
			if d.cfg.Viper.IsSet("daemon.broker_readiness_initial_delay") {
				readinessInitialDelay = d.cfg.Viper.GetDuration("daemon.broker_readiness_initial_delay")
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
		mqttBrokerAddr = d.cfg.MqttBroker
	}

	// Connect to the network's MQTT broker or use the embedded broker
	err = mqtt.NewClientE(d.ctx, mqttBrokerAddr, myhome.InstanceName, d.cfg.MdnsTimeout, d.cfg.MqttTimeout, d.cfg.MqttGrace, d.cfg.MqttReconnectInterval, d.cfg.MqttWatchdogInterval, d.cfg.MqttWatchdogMaxFailures, false)
	if err != nil {
		log.Error(err, "Failed to initialize MQTT client")
		return err
	}

	// Start the MQTT client and get the client instance
	mc, err := mqtt.GetClientE(d.ctx)
	if err != nil {
		log.Error(err, "Failed to start MQTT client")
		return err
	}
	defer mc.Close()

	shelly.Init(log, mc, d.cfg.MqttTimeout, d.cfg.ShellyRateLimit, scripts.GetFS())
	// Store mc in d.ctx for pkg/shelly's own mqtt.GetClient(ctx) callers
	// (pkg/shelly/device.go, pkg/shelly/script/run.go) — see
	// pkg/shelly/mqtt.NewContextWithClient's doc comment. Every subsequent
	// use of d.ctx in this method and in goroutines spawned after this
	// point sees the wrapped value.
	d.ctx = shellymqtt.NewContextWithClient(d.ctx, mc)

	// Start the main HTTP server (as a Mux), given to every other servers started below
	// mux := http.NewServeMux()

	// Start Gen1 (HTTP->MQTT) proxy (auto-enabled with embedded MQTT broker)
	if d.cfg.EnableGen1Proxy {
		log.Info("Starting Gen1 (HTTP->MQTT) proxy")
		gen1.StartHttp2MqttProxy(logr.NewContext(d.ctx, log.WithName("gen1")), 8888, mc)
	} else {
		log.Info("Gen1 (HTTP->MQTT) proxy disabled")
	}

	// Start Occupancy service (MQTT only)
	if d.cfg.EnableOccupancyService {
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
	if d.cfg.EnableMetricsExporter {
		log.Info("Starting Prometheus metrics exporter")

		httpAddr := fmt.Sprintf(":%d", d.cfg.MetricsExporterPort)
		exporter := metrics.NewExporter(
			d.ctx,
			log.WithName("metrics"),
			mc,
			d.cfg.MetricsExporterTopic,
			httpAddr,
		)

		if err := exporter.Start(); err != nil {
			log.Error(err, "Failed to start metrics exporter")
			return err
		}

		log.Info("Prometheus metrics exporter started",
			"http_addr", httpAddr,
			"mqtt_topic", d.cfg.MetricsExporterTopic)
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
	beemEnabled := d.cfg.BeemEmail != "" && d.cfg.BeemPassword != ""
	accountsRegistry.SetEnabled("beem", beemEnabled)
	if beemEnabled {
		log.Info("Starting Beem Energy watcher")
		beemCfg := beem.ClientConfig{
			Email:        d.cfg.BeemEmail,
			Password:     d.cfg.BeemPassword,
			PollInterval: d.cfg.BeemPollInterval,
		}
		beemWatcher = beem.NewWatcher(d.ctx, beemCfg, mc, log.WithName("pkg/beem"))
		beemWatcher.OnResult = func(err error) { accountsRegistry.Report("beem", err) }
		if err := beemWatcher.Start(d.ctx); err != nil {
			log.Error(err, "Failed to start Beem watcher")
			return err
		}
		log.Info("Beem Energy watcher started")
	} else {
		log.Info("Beem Energy integration disabled")
	}

	// Solar aggregator: sums whatever solar-energy sources are known (today:
	// only Beem) and republishes the total on a retained MQTT topic for
	// Shelly device scripts to consume directly. Generic and additive — no
	// dependency on PoolDeviceID/pool tracking, so it is not gated behind any
	// pool-related flag.
	var solarAgg *SolarAggregator
	if beemWatcher != nil {
		solarAgg = NewSolarAggregator(log.WithName("solar"), mc, d.cfg.SolarStaleAfter,
			newBeemSolarSource(beemWatcher),
			// future: append additional SolarSource adapters here
		)
		solarAgg.Start(d.ctx)
		log.Info("Solar aggregator started", "topic", SolarAvailableTopic, "stale_after", d.cfg.SolarStaleAfter)
	} else {
		log.Info("Solar aggregator disabled: no solar sources configured (Beem credentials absent)")
	}

	// SFR box: the device manager (below) starts a periodic refresh loop via
	// myhomesfr.GetRouter regardless of credentials — auth is skipped
	// internally when username/password are empty. Report status from every
	// refresh attempt; SetStatusReporter must be called before the device
	// manager starts (it triggers the first refresh synchronously).
	sfrEnabled := d.cfg.SFRUsername != "" && d.cfg.SFRPassword != ""
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

		if d.cfg.EnableEventsService {
			eventsStore, err = events.NewStorage(log.WithName("events"), d.cfg.EventsDBPath)
			if err != nil {
				log.Error(err, "Failed to initialize events storage", "path", d.cfg.EventsDBPath)
				// Non-fatal: continue without event recording
				eventsStore = nil
			} else {
				eventsTracker = events.NewSensorDailyTracker(log.WithName("events"), eventsStore)
				eventsSvc = events.NewService(log.WithName("events"), eventsStore, eventsTracker, broadcastFn, d.cfg.EventsRetention)
				go func() {
					if err := eventsTracker.Start(d.ctx); err != nil {
						log.Error(err, "Events tracker stopped")
					}
				}()
				go func() {
					if err := eventsSvc.Start(d.ctx); err != nil {
						log.Error(err, "Events service stopped")
					}
				}()
				log.Info("Events service started", "db", d.cfg.EventsDBPath, "retention", d.cfg.EventsRetention)
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
		if d.cfg.EnableNoticeService {
			if eventsSvc == nil {
				log.Info("Notice service disabled: events service is not running")
			} else if d.occupancyService == nil {
				log.Info("Notice service disabled: occupancy service is not running")
			} else {
				accountsRegistry.SetEnabled("smtp", d.cfg.SMTPFrom != "")
				mailer := &reportingMailer{
					Mailer: notify.New(log.WithName("notify"), notify.Config{
						Host:     d.cfg.SMTPHost,
						Port:     d.cfg.SMTPPort,
						Username: d.cfg.SMTPUsername,
						Password: d.cfg.SMTPPassword,
						From:     d.cfg.SMTPFrom,
						To:       d.cfg.SMTPTo,
					}),
					registry: accountsRegistry,
				}
				noticeSvc = notice.NewService(log.WithName("notice"), eventsSvc, d.occupancyService, mailer, notice.Config{
					NightStart: d.cfg.NoticeNightStart,
					NightEnd:   d.cfg.NoticeNightEnd,
					DigestHour: d.cfg.NoticeDigestHour,
				})
				go noticeSvc.Start(d.ctx)
				log.Info("Notice service started",
					"night_start", d.cfg.NoticeNightStart,
					"night_end", d.cfg.NoticeNightEnd,
					"digest_hour", d.cfg.NoticeDigestHour,
					"email_enabled", d.cfg.SMTPFrom != "",
				)
			}
		} else {
			log.Info("Notice service disabled")
		}

		// Record a "pool.turnover_today" notice whenever the pump stops
		// (schedule/manual or solar-triggered pool.pump_stop — solar-driven
		// stops flow through pool-pump.js's own doStop since #405, so there is
		// no separate daemon-emitted solar event to watch for here anymore).
		// Degraded mode: if the pool device can't be reached, NewPoolNotices
		// returns nil and broadcastFn's poolNotices.OnEvent call is then a
		// no-op — pump control itself is entirely unaffected either way.
		if d.cfg.PoolEnabled && d.cfg.PoolDeviceID != "" {
			poolNotices = NewPoolNotices(d.ctx, log.WithName("pool"), eventsSvc, d.cfg.PoolDeviceID)
		}

		// Minimal static identity registry of things that may claim solar
		// energy (today: only the pool pump). Not a live-arbitration engine —
		// see internal/myhome/energy and #401's follow-up "solar router" issue.
		claimerRegistry := energy.NewRegistry()
		if d.cfg.PoolDeviceID != "" {
			claimerRegistry.Register("pool-pump", d.cfg.PoolDeviceID)
		}

		// Start device manager
		d.dm = impl.NewDeviceManager(d.ctx, d.registry, impl.Config{
			RemoteProxy:       d.cfg.RemoteProxy,
			RefreshInterval:   d.cfg.RefreshInterval,
			EventsDir:         d.cfg.EventsDir,
			MqttBroker:        d.cfg.MqttBroker,
			AutoSetup:         d.cfg.AutoSetup,
			ReconcileInterval: d.cfg.ReconcileInterval,
			MdnsTimeout:       d.cfg.MdnsTimeout,
		}, storage, resolver, mc, sseBroadcaster)
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
			go func() {
				if err := gen2Listener.Start(d.ctx); err != nil {
					log.Error(err, "Gen2 event listener stopped")
				}
			}()
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
			myhome.Register(d.registry, myhome.EventList, func(ctx context.Context, req *myhome.EventListRequest) (*myhome.EventListResponse, error) {
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
		if d.cfg.EnableTemperatureService {
			log.Info("Initializing temperature RPC methods")

			// Create temperature storage using the same database
			tempStorage, err := temperature.NewStorage(log, storage.DB())
			if err != nil {
				log.Error(err, "Failed to initialize temperature storage")
				return err
			}

			// Create and register temperature method handlers, republishing temperature ranges at startup
			tempHandlers := temperature.NewService(d.ctx, log, d.registry, mc, tempStorage)
			tempHandlers.RegisterHandlers()

			log.Info("Temperature RPC methods registered")
		}

		// Register the fetch-and-transform proxy (#465). Always registered —
		// like the Pool/Solar RPC handlers below, there is nothing to
		// disable: with zero subscriptions it is an idle MQTT subscriber and
		// an empty fetch.list.
		log.Info("Initializing fetch-and-transform proxy")
		fetchStorage, err := fetchproxy.NewStorage(log, storage.DB())
		if err != nil {
			log.Error(err, "Failed to initialize fetch-and-transform proxy storage")
			return err
		}
		fetchService := fetchproxy.NewService(d.ctx, log, d.registry, mc, fetchStorage)
		fetchService.RegisterHandlers()
		log.Info("Fetch-and-transform proxy RPC methods registered")

		// Register Occupancy RPC methods if enabled
		if d.cfg.EnableOccupancyService && d.occupancyService != nil {
			log.Info("Registering occupancy RPC methods")

			// Create and register occupancy RPC handler
			occupancyHandler := occupancy.NewRPCHandler(log, d.occupancyService, d.registry)
			occupancyHandler.RegisterHandlers()

			log.Info("Occupancy RPC methods registered")
		}

		// Register Pool RPC methods (turnover rate + water-supply status for
		// the UI and `ctl pool status`). Always registered — the signature
		// exists regardless of pool tracking; handleGetStatus itself returns
		// a clear error if poolNotices is nil (pool disabled/unreachable).
		poolRPCHandler := NewPoolRPCHandler(log, poolNotices, d.registry)
		poolRPCHandler.RegisterHandlers()

		// Register Solar RPC methods (static energy-claimers registry).
		// Always registered — claimerRegistry may simply be empty when no
		// pool device is configured.
		solarRPCHandler := NewSolarRPCHandler(log, claimerRegistry, poolNotices, d.registry)
		solarRPCHandler.RegisterHandlers()

		// Publish a hostname for the DeviceManager host: myhome.local
		if !d.cfg.NoMdnsPublish {
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
		if err := ui.Start(d.ctx, log.WithName("server"), d.cfg.UiPort, resolver, d.dm, mc, sseBroadcaster, eventsSvc, d.cfg.RemoteProxy, accountsRegistry, global.PanicOnBugs, d.registry); err != nil {
			log.Error(err, "Failed to start UI server")
			return err
		}

	} else {
		log.Info("Device manager disabled")
	}

	log.Info("Running")

	// Start periodic MQTT connection status monitoring if enabled
	if d.cfg.MqttBrokerClientLogInterval > 0 {
		go func(ctx context.Context) {
			log := logr.FromContextOrDiscard(ctx)
			ticker := time.NewTicker(d.cfg.MqttBrokerClientLogInterval)
			defer ticker.Stop()

			log.Info("Starting MQTT connection status monitoring", "interval", d.cfg.MqttBrokerClientLogInterval)

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
