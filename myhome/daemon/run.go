package daemon

import (
	"myhome/ctl/options"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var disableGen1Proxy bool
var disableOccupancyService bool

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
	runCmd.PersistentFlags().StringVarP(&options.Flags.EventsDir, "events-dir", "E", "", "Directory to write received MQTT events as JSON files")
	runCmd.PersistentFlags().IntVarP(&options.Flags.ProxyPort, "proxy-port", "p", 6080, "Reverse proxy listen port (default 6080)")
	runCmd.PersistentFlags().BoolVar(&options.Flags.EnableGen1Proxy, "enable-gen1-proxy", false, "Enable the Gen1 HTTP->MQTT proxy (requires embedded broker)")
	runCmd.PersistentFlags().BoolVar(&disableGen1Proxy, "disable-gen1-proxy", false, "Disable the Gen1 HTTP->MQTT proxy (mutually exclusive with --enable-gen1-proxy)")
	runCmd.PersistentFlags().BoolVar(&options.Flags.EnableOccupancyService, "enable-occupancy-service", false, "Enable the occupancy HTTP service on port 8889")
	runCmd.PersistentFlags().BoolVar(&disableOccupancyService, "disable-occupancy-service", false, "Disable the occupancy HTTP service (mutually exclusive with --enable-occupancy-service)")
	runCmd.MarkFlagsMutuallyExclusive("enable-gen1-proxy", "disable-gen1-proxy")
	runCmd.MarkFlagsMutuallyExclusive("enable-occupancy-service", "disable-occupancy-service")
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

		// Handle Gen1 proxy flags with mutual exclusivity
		if cmd.Flags().Changed("disable-gen1-proxy") && disableGen1Proxy {
			options.Flags.EnableGen1Proxy = false
		} else if !cmd.Flags().Changed("enable-gen1-proxy") && !cmd.Flags().Changed("disable-gen1-proxy") {
			log.Info("Setting enable-gen1-proxy based on mqtt-broker flag")
			options.Flags.EnableGen1Proxy = options.Flags.MqttBroker == ""
		}

		// Handle occupancy service flags with mutual exclusivity
		if cmd.Flags().Changed("disable-occupancy-service") && disableOccupancyService {
			options.Flags.EnableOccupancyService = false
		} else if !cmd.Flags().Changed("enable-occupancy-service") && !cmd.Flags().Changed("disable-occupancy-service") {
			log.Info("Setting enable-occupancy-service based on mqtt-broker flag")
			options.Flags.EnableOccupancyService = options.Flags.MqttBroker == ""
		}

		daemon := NewDaemon(ctx)
		log.Info("Running in foreground")
		return daemon.Run()
	},
}
