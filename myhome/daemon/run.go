package daemon

import (
	"strconv"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/asnowfix/home-automation/pkg/sfr"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func defaultEventsDBPath() string {
	return "events.db"
}

var disableGen1Proxy bool
var disableOccupancyService bool
var disableTemperatureService bool
var disableAutoSetup bool
var disableEventsService bool

func init() {
	Cmd.AddCommand(runCmd)

	runCmd.Flags().BoolVarP(&disableDeviceManager, "disable-device-manager", "D", false, "Disable the device manager")
	runCmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	runCmd.PersistentFlags().DurationVarP(&options.Flags.MdnsTimeout, "mdns-timeout", "M", options.MDNS_LOOKUP_DEFAULT_TIMEOUT, "Timeout for mDNS lookups")
	runCmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", options.MQTT_DEFAULT_TIMEOUT, "Timeout for MQTT operations")
	runCmd.PersistentFlags().DurationVarP(&options.Flags.MqttGrace, "mqtt-grace", "G", options.MQTT_DEFAULT_GRACE, "MQTT disconnection grace period")
	runCmd.PersistentFlags().DurationVarP(&options.Flags.RefreshInterval, "refresh-interval", "R", options.DEVICE_REFRESH_INTERVAL, "Known devices refresh interval")
	runCmd.PersistentFlags().DurationVarP(&options.Flags.MqttWatchdogInterval, "mqtt-watchdog-interval", "W", options.MQTT_WATCHDOG_CHECK_INTERVAL, "MQTT watchdog check interval")
	runCmd.PersistentFlags().IntVarP(&options.Flags.MqttWatchdogMaxFailures, "mqtt-watchdog-max-failures", "F", options.MQTT_WATCHDOG_MAX_FAILURES, "MQTT watchdog max consecutive failures before restart")
	runCmd.PersistentFlags().DurationVar(&options.Flags.MqttReconnectInterval, "mqtt-reconnect-interval", options.MQTT_RECONNECT_INTERVAL, "Interval for periodic MQTT reconnection to refresh retained messages (0 to disable)")
	runCmd.PersistentFlags().DurationVar(&options.Flags.MqttBrokerClientLogInterval, "mqtt-broker-client-log-interval", options.MQTT_BROKER_CLIENT_LOG_INTERVAL, "Interval for logging MQTT broker connected clients (0 to disable)")
	runCmd.PersistentFlags().StringVarP(&options.Flags.EventsDir, "events-dir", "E", "", "Directory to write received MQTT events as JSON files")
	runCmd.PersistentFlags().IntVarP(&options.Flags.UiPort, "ui-port", "p", options.HTTP_DEFAULT_PORT, "UI listen port (default "+strconv.Itoa(options.HTTP_DEFAULT_PORT)+")")
	runCmd.PersistentFlags().BoolVar(&options.Flags.EnableGen1Proxy, "enable-gen1-proxy", false, "Enable the Gen1 HTTP->MQTT proxy (requires embedded broker)")
	runCmd.PersistentFlags().BoolVar(&disableGen1Proxy, "disable-gen1-proxy", false, "Disable the Gen1 HTTP->MQTT proxy (mutually exclusive with --enable-gen1-proxy)")
	runCmd.PersistentFlags().BoolVar(&options.Flags.EnableOccupancyService, "enable-occupancy-service", false, "Enable the occupancy service (auto-enabled with device manager)")
	runCmd.PersistentFlags().BoolVar(&disableOccupancyService, "disable-occupancy-service", false, "Disable the occupancy service (mutually exclusive with --enable-occupancy-service)")
	runCmd.PersistentFlags().BoolVar(&options.Flags.EnableTemperatureService, "enable-temperature-service", false, "Enable the temperature service (auto-enabled with device manager)")
	runCmd.PersistentFlags().BoolVar(&disableTemperatureService, "disable-temperature-service", false, "Disable the temperature service (mutually exclusive with --enable-temperature-service)")
	runCmd.PersistentFlags().BoolVar(&options.Flags.EnableMetricsExporter, "enable-metrics-exporter", false, "Enable the Prometheus metrics exporter (auto-enabled with device manager)")
	runCmd.PersistentFlags().IntVar(&options.Flags.MetricsExporterPort, "metrics-exporter-port", options.PROMETHEUS_DEFAULT_PORT, "Prometheus metrics exporter HTTP port")
	runCmd.PersistentFlags().StringVar(&options.Flags.MetricsExporterTopic, "metrics-exporter-topic", "shelly/metrics", "MQTT topic for Shelly device metrics")
	runCmd.PersistentFlags().BoolVar(&disableAutoSetup, "disable-auto-setup", false, "Disable automatic configuration of newly discovered unknown devices")
	runCmd.PersistentFlags().DurationVar(&options.Flags.ReconcileInterval, "reconcile-interval", options.RECONCILE_DEFAULT_INTERVAL, "Interval for re-applying canonical MQTT broker/NTP/Matter config to known devices over HTTP (0 to disable)")
	runCmd.PersistentFlags().BoolVar(&options.Flags.NoMdnsPublish, "no-mdns-publish", false, "Disable mDNS/Zeroconf publishing (useful for dev instances)")
	runCmd.PersistentFlags().StringVarP(&options.Flags.InstanceName, "instance", "I", "myhome", "Server instance name for RPC topics (default: myhome)")
	runCmd.PersistentFlags().StringVar(&options.Flags.EventsDBPath, "events-db", defaultEventsDBPath(), "Path to the events SQLite database")
	runCmd.PersistentFlags().DurationVar(&options.Flags.EventsRetention, "events-retention", 90*24*time.Hour, "Retention period for event records (default 90 days)")
	runCmd.PersistentFlags().BoolVar(&disableEventsService, "disable-events-service", false, "Disable the event recording service")
	runCmd.PersistentFlags().StringVar(&options.Flags.RemoteProxy, "remote-proxy", "", "Forward /devices/... requests to a remote myhome daemon (e.g. http://home-pi:6080) instead of connecting directly")
	runCmd.PersistentFlags().StringVar(&options.Flags.PoolDeviceID, "pool-device-id", "", "Pool Shelly device ID")
	runCmd.PersistentFlags().BoolVar(&options.Flags.PoolEnabled, "enable-pool", false, "Enable pool runtime tracking")
	runCmd.PersistentFlags().BoolVar(&options.Flags.PoolSolarEnabled, "enable-pool-solar", false, "Enable solar-driven pool pump automation")
	runCmd.PersistentFlags().Float64Var(&options.Flags.PoolSolarStartThresholdW, "pool-solar-start-threshold-w", 500, "Solar power threshold to start pump (W)")
	runCmd.PersistentFlags().Float64Var(&options.Flags.PoolSolarStopThresholdW, "pool-solar-stop-threshold-w", 200, "Solar power threshold to stop pump (W)")
	runCmd.PersistentFlags().DurationVar(&options.Flags.PoolSolarStartDelay, "pool-solar-start-delay", 5*time.Minute, "Solar must hold above start threshold for this long before starting pump")
	runCmd.PersistentFlags().DurationVar(&options.Flags.PoolSolarStopDelay, "pool-solar-stop-delay", 10*time.Minute, "Solar must hold below stop threshold for this long before stopping pump")
	runCmd.PersistentFlags().Float64Var(&options.Flags.PoolSolarMinVolumeTurnover, "pool-solar-min-volume-turnover", 5, "Soft-stop target: pool volumes filtered per day; pump keeps running past this while solar is still above start threshold")
	runCmd.PersistentFlags().Float64Var(&options.Flags.PoolSolarMaxVolumeTurnover, "pool-solar-max-volume-turnover", 7, "Hard ceiling: pool volumes filtered per day; pump always stops (and won't be solar-started) once reached")
	runCmd.PersistentFlags().BoolVar(&options.Flags.EnableNoticeService, "enable-notice-service", false, "Enable the notice service (motion rule + daily email digest); requires the events and occupancy services")
	runCmd.PersistentFlags().StringVar(&options.Flags.NoticeNightStart, "notice-night-start", "22:00", "Night window start (HH:MM) used by the motion notice rule")
	runCmd.PersistentFlags().StringVar(&options.Flags.NoticeNightEnd, "notice-night-end", "06:00", "Night window end (HH:MM) used by the motion notice rule")
	runCmd.PersistentFlags().IntVar(&options.Flags.NoticeDigestHour, "notice-digest-hour", 8, "Local hour (0-23) at which the daily notice digest email is sent")
	runCmd.PersistentFlags().StringVar(&options.Flags.SMTPHost, "smtp-host", "smtp.gmail.com", "SMTP host for the notice digest email")
	runCmd.PersistentFlags().IntVar(&options.Flags.SMTPPort, "smtp-port", 587, "SMTP port for the notice digest email (STARTTLS submission)")
	runCmd.MarkFlagsMutuallyExclusive("enable-gen1-proxy", "disable-gen1-proxy")
	runCmd.MarkFlagsMutuallyExclusive("enable-occupancy-service", "disable-occupancy-service")
	runCmd.MarkFlagsMutuallyExclusive("enable-temperature-service", "disable-temperature-service")
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run MyHome in foreground",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log, err := logr.FromContext(ctx)
		if err != nil {
			return err
		}

		// Initialize Viper and bind to flags
		v := viper.New()
		v.SetConfigName("myhome")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/myhome/")
		v.AddConfigPath("$HOME/.myhome/")

		// Enable environment variable support
		v.SetEnvPrefix("MYHOME")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		v.AutomaticEnv()
		v.SetDefault("beem.poll_interval", "60s")

		// Try to read config file (optional)
		if err := v.ReadInConfig(); err == nil {
			log.Info("Loaded config from", "file", v.ConfigFileUsed())
		} else {
			log.Info("No config file found, using defaults and flags")
		}

		// Bind flags to viper (flags take precedence over config file)
		if err := v.BindPFlags(cmd.Flags()); err != nil {
			log.Error(err, "Failed to bind flags to viper")
		}
		if err := v.BindPFlags(cmd.PersistentFlags()); err != nil {
			log.Error(err, "Failed to bind persistent flags to viper")
		}

		// Read configuration values (prefer flags, then config file, then defaults)
		if v.IsSet("daemon.mqtt_broker") && !cmd.Flags().Changed("mqtt-broker") {
			options.Flags.MqttBroker = v.GetString("daemon.mqtt_broker")
		}
		if v.IsSet("daemon.mdns_timeout") && !cmd.Flags().Changed("mdns-timeout") {
			options.Flags.MdnsTimeout = v.GetDuration("daemon.mdns_timeout")
		}
		if v.IsSet("daemon.mqtt_timeout") && !cmd.Flags().Changed("mqtt-timeout") {
			options.Flags.MqttTimeout = v.GetDuration("daemon.mqtt_timeout")
		}
		if v.IsSet("daemon.mqtt_grace") && !cmd.Flags().Changed("mqtt-grace") {
			options.Flags.MqttGrace = v.GetDuration("daemon.mqtt_grace")
		}
		if v.IsSet("daemon.refresh_interval") && !cmd.Flags().Changed("refresh-interval") {
			options.Flags.RefreshInterval = v.GetDuration("daemon.refresh_interval")
		}
		if v.IsSet("daemon.mqtt_watchdog_interval") && !cmd.Flags().Changed("mqtt-watchdog-interval") {
			options.Flags.MqttWatchdogInterval = v.GetDuration("daemon.mqtt_watchdog_interval")
		}
		if v.IsSet("daemon.mqtt_watchdog_max_failures") && !cmd.Flags().Changed("mqtt-watchdog-max-failures") {
			options.Flags.MqttWatchdogMaxFailures = v.GetInt("daemon.mqtt_watchdog_max_failures")
		}
		if v.IsSet("daemon.mqtt_reconnect_interval") && !cmd.Flags().Changed("mqtt-reconnect-interval") {
			options.Flags.MqttReconnectInterval = v.GetDuration("daemon.mqtt_reconnect_interval")
		}
		if v.IsSet("daemon.mqtt_broker_client_log_interval") && !cmd.Flags().Changed("mqtt-broker-client-log-interval") {
			options.Flags.MqttBrokerClientLogInterval = v.GetDuration("daemon.mqtt_broker_client_log_interval")
		}
		if v.IsSet("daemon.reconcile_interval") && !cmd.Flags().Changed("reconcile-interval") {
			options.Flags.ReconcileInterval = v.GetDuration("daemon.reconcile_interval")
		}
		if v.IsSet("daemon.events_dir") && !cmd.Flags().Changed("events-dir") {
			options.Flags.EventsDir = v.GetString("daemon.events_dir")
		}
		if v.IsSet("daemon.ui_port") && !cmd.Flags().Changed("ui-port") {
			options.Flags.UiPort = v.GetInt("daemon.ui_port")
		}
		if v.IsSet("daemon.enable_gen1_proxy") && !cmd.Flags().Changed("enable-gen1-proxy") {
			options.Flags.EnableGen1Proxy = v.GetBool("daemon.enable_gen1_proxy")
		}
		if v.IsSet("daemon.enable_occupancy_service") && !cmd.Flags().Changed("enable-occupancy-service") {
			options.Flags.EnableOccupancyService = v.GetBool("daemon.enable_occupancy_service")
		}
		if v.IsSet("daemon.enable_temperature_service") && !cmd.Flags().Changed("enable-temperature-service") {
			options.Flags.EnableTemperatureService = v.GetBool("daemon.enable_temperature_service")
		}
		if v.IsSet("daemon.disable_device_manager") && !cmd.Flags().Changed("disable-device-manager") {
			disableDeviceManager = v.GetBool("daemon.disable_device_manager")
		}
		if v.IsSet("daemon.remote_proxy") && !cmd.Flags().Changed("remote-proxy") {
			options.Flags.RemoteProxy = v.GetString("daemon.remote_proxy")
		}
		// Handle auto-setup flag (default is enabled, --disable-auto-setup disables it)
		// Config file can also disable it via daemon.disable_auto_setup: true
		if cmd.Flags().Changed("disable-auto-setup") && disableAutoSetup {
			options.Flags.AutoSetup = false
		} else if v.IsSet("daemon.disable_auto_setup") && v.GetBool("daemon.disable_auto_setup") {
			options.Flags.AutoSetup = false
		} else {
			// Default: auto-setup is enabled
			options.Flags.AutoSetup = true
		}

		// Handle Gen1 proxy flags with mutual exclusivity
		if cmd.Flags().Changed("disable-gen1-proxy") && disableGen1Proxy {
			options.Flags.EnableGen1Proxy = false
		} else if !cmd.Flags().Changed("enable-gen1-proxy") && !cmd.Flags().Changed("disable-gen1-proxy") {
			log.Info("Setting enable-gen1-proxy based on mqtt-broker flag")
			options.Flags.EnableGen1Proxy = options.Flags.MqttBroker == ""
		}

		// Handle occupancy service flags with mutual exclusivity
		// Auto-enable if device manager is enabled and not explicitly disabled
		if cmd.Flags().Changed("disable-occupancy-service") && disableOccupancyService {
			options.Flags.EnableOccupancyService = false
		} else if !cmd.Flags().Changed("enable-occupancy-service") && !cmd.Flags().Changed("disable-occupancy-service") {
			// Auto-enable with device manager
			options.Flags.EnableOccupancyService = !disableDeviceManager
			if options.Flags.EnableOccupancyService {
				log.Info("Auto-enabling occupancy service (device manager enabled)")
			}
		}

		// Handle temperature service flags with mutual exclusivity
		// Auto-enable if device manager is enabled and not explicitly disabled
		if cmd.Flags().Changed("disable-temperature-service") && disableTemperatureService {
			options.Flags.EnableTemperatureService = false
		} else if !cmd.Flags().Changed("enable-temperature-service") && !cmd.Flags().Changed("disable-temperature-service") {
			// Auto-enable with device manager
			options.Flags.EnableTemperatureService = !disableDeviceManager
			if options.Flags.EnableTemperatureService {
				log.Info("Auto-enabling temperature service (device manager enabled)")
			}
		}

		// Handle events service config from viper / flags
		if v.IsSet("events.db") && !cmd.Flags().Changed("events-db") {
			options.Flags.EventsDBPath = v.GetString("events.db")
		}
		if v.IsSet("events.retention") && !cmd.Flags().Changed("events-retention") {
			options.Flags.EventsRetention = v.GetDuration("events.retention")
		}
		if cmd.Flags().Changed("disable-events-service") && disableEventsService {
			options.Flags.EnableEventsService = false
		} else if v.IsSet("events.enabled") && !v.GetBool("events.enabled") {
			options.Flags.EnableEventsService = false
		} else {
			options.Flags.EnableEventsService = !disableDeviceManager
			if options.Flags.EnableEventsService {
				log.Info("Auto-enabling events service (device manager enabled)")
			}
		}

		// Handle notice service config from viper / flags. Unlike events/
		// occupancy/temperature, notice is not auto-enabled with the device
		// manager — it depends on both of those already being enabled and
		// on (optional) SMTP credentials, so an operator opts in explicitly.
		if v.IsSet("notice.enabled") && !cmd.Flags().Changed("enable-notice-service") {
			options.Flags.EnableNoticeService = v.GetBool("notice.enabled")
		}
		if v.IsSet("notice.night_start") && !cmd.Flags().Changed("notice-night-start") {
			options.Flags.NoticeNightStart = v.GetString("notice.night_start")
		}
		if v.IsSet("notice.night_end") && !cmd.Flags().Changed("notice-night-end") {
			options.Flags.NoticeNightEnd = v.GetString("notice.night_end")
		}
		if v.IsSet("notice.digest_hour") && !cmd.Flags().Changed("notice-digest-hour") {
			options.Flags.NoticeDigestHour = v.GetInt("notice.digest_hour")
		}
		if v.IsSet("smtp.host") && !cmd.Flags().Changed("smtp-host") {
			options.Flags.SMTPHost = v.GetString("smtp.host")
		}
		if v.IsSet("smtp.port") && !cmd.Flags().Changed("smtp-port") {
			options.Flags.SMTPPort = v.GetInt("smtp.port")
		}

		// SMTP credentials — Viper reads MYHOME_SMTP_USERNAME / MYHOME_SMTP_PASSWORD /
		// MYHOME_SMTP_FROM / MYHOME_SMTP_TO from the environment or config file.
		// Like Beem/SFR above, these are never CLI flags. Email sending is
		// skipped entirely (not an error) when smtp.from is empty — see
		// myhome/notify.New.
		options.Flags.SMTPUsername = v.GetString("smtp.username")
		options.Flags.SMTPPassword = v.GetString("smtp.password")
		options.Flags.SMTPFrom = v.GetString("smtp.from")
		options.Flags.SMTPTo = v.GetString("smtp.to")

		// Handle pool runtime tracker config from viper / flags
		if v.IsSet("pool.device_id") && !cmd.Flags().Changed("pool-device-id") {
			options.Flags.PoolDeviceID = v.GetString("pool.device_id")
		}
		if v.IsSet("pool.enabled") && !cmd.Flags().Changed("enable-pool") {
			options.Flags.PoolEnabled = v.GetBool("pool.enabled")
		}

		// Beem Energy: enabled automatically when both email and password are set
		options.Flags.BeemEmail = v.GetString("beem.email")
		options.Flags.BeemPassword = v.GetString("beem.password")
		options.Flags.BeemPollInterval = v.GetDuration("beem.poll_interval")

		// SFR box credentials — Viper reads MYHOME_SFR_USERNAME / MYHOME_SFR_PASSWORD
		// from the environment or config file; auth is skipped if either is empty.
		sfr.Init(v.GetString("sfr.username"), v.GetString("sfr.password"))

		// Handle pool solar automation config from viper / flags
		if v.IsSet("pool.solar.enabled") && !cmd.Flags().Changed("enable-pool-solar") {
			options.Flags.PoolSolarEnabled = v.GetBool("pool.solar.enabled")
		}
		if v.IsSet("pool.solar.start_threshold_w") && !cmd.Flags().Changed("pool-solar-start-threshold-w") {
			options.Flags.PoolSolarStartThresholdW = v.GetFloat64("pool.solar.start_threshold_w")
		}
		if v.IsSet("pool.solar.stop_threshold_w") && !cmd.Flags().Changed("pool-solar-stop-threshold-w") {
			options.Flags.PoolSolarStopThresholdW = v.GetFloat64("pool.solar.stop_threshold_w")
		}
		if v.IsSet("pool.solar.start_delay") && !cmd.Flags().Changed("pool-solar-start-delay") {
			options.Flags.PoolSolarStartDelay = v.GetDuration("pool.solar.start_delay")
		}
		if v.IsSet("pool.solar.stop_delay") && !cmd.Flags().Changed("pool-solar-stop-delay") {
			options.Flags.PoolSolarStopDelay = v.GetDuration("pool.solar.stop_delay")
		}
		if v.IsSet("pool.solar.min_volume_turnover") && !cmd.Flags().Changed("pool-solar-min-volume-turnover") {
			options.Flags.PoolSolarMinVolumeTurnover = v.GetFloat64("pool.solar.min_volume_turnover")
		}
		if v.IsSet("pool.solar.max_volume_turnover") && !cmd.Flags().Changed("pool-solar-max-volume-turnover") {
			options.Flags.PoolSolarMaxVolumeTurnover = v.GetFloat64("pool.solar.max_volume_turnover")
		}

		// Store Viper instance in global options for daemon to use
		options.ViperConfig = v

		daemon := NewDaemon(ctx)
		log.Info("Running in foreground")
		return daemon.Run()
	},
}
