package daemon

import (
	"myhome/ctl/options"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var disableGen1Proxy bool
var disableOccupancyService bool
var disableTemperatureService bool
var disableAutoSetup bool

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
	runCmd.PersistentFlags().DurationVar(&options.Flags.MqttBrokerClientLogInterval, "mqtt-broker-client-log-interval", options.MQTT_BROKER_CLIENT_LOG_INTERVAL, "Interval for logging MQTT broker connected clients (0 to disable)")
	runCmd.PersistentFlags().StringVarP(&options.Flags.EventsDir, "events-dir", "E", "", "Directory to write received MQTT events as JSON files")
	runCmd.PersistentFlags().IntVarP(&options.Flags.UiPort, "ui-port", "p", 6080, "UI listen port (default 6080)")
	runCmd.PersistentFlags().BoolVar(&options.Flags.EnableGen1Proxy, "enable-gen1-proxy", false, "Enable the Gen1 HTTP->MQTT proxy (requires embedded broker)")
	runCmd.PersistentFlags().BoolVar(&disableGen1Proxy, "disable-gen1-proxy", false, "Disable the Gen1 HTTP->MQTT proxy (mutually exclusive with --enable-gen1-proxy)")
	runCmd.PersistentFlags().BoolVar(&options.Flags.EnableOccupancyService, "enable-occupancy-service", false, "Enable the occupancy service (auto-enabled with device manager)")
	runCmd.PersistentFlags().BoolVar(&disableOccupancyService, "disable-occupancy-service", false, "Disable the occupancy service (mutually exclusive with --enable-occupancy-service)")
	runCmd.PersistentFlags().BoolVar(&options.Flags.EnableTemperatureService, "enable-temperature-service", false, "Enable the temperature service (auto-enabled with device manager)")
	runCmd.PersistentFlags().BoolVar(&disableTemperatureService, "disable-temperature-service", false, "Disable the temperature service (mutually exclusive with --enable-temperature-service)")
	runCmd.PersistentFlags().BoolVar(&options.Flags.EnableMetricsExporter, "enable-metrics-exporter", false, "Enable the Prometheus metrics exporter (auto-enabled with device manager)")
	runCmd.PersistentFlags().IntVar(&options.Flags.MetricsExporterPort, "metrics-exporter-port", 9100, "Prometheus metrics exporter HTTP port")
	runCmd.PersistentFlags().StringVar(&options.Flags.MetricsExporterTopic, "metrics-exporter-topic", "shelly/metrics", "MQTT topic for Shelly device metrics")
	runCmd.PersistentFlags().BoolVar(&disableAutoSetup, "disable-auto-setup", false, "Disable automatic configuration of newly discovered unknown devices")
	runCmd.PersistentFlags().BoolVar(&options.Flags.NoMdnsPublish, "no-mdns-publish", false, "Disable mDNS/Zeroconf publishing (useful for dev instances)")
	runCmd.PersistentFlags().StringVarP(&options.Flags.InstanceName, "instance", "I", "myhome", "Server instance name for RPC topics (default: myhome)")
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
		v.AutomaticEnv()

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
		if v.IsSet("daemon.mqtt_broker_client_log_interval") && !cmd.Flags().Changed("mqtt-broker-client-log-interval") {
			options.Flags.MqttBrokerClientLogInterval = v.GetDuration("daemon.mqtt_broker_client_log_interval")
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

		daemon := NewDaemon(ctx)
		log.Info("Running in foreground")
		return daemon.Run()
	},
}
