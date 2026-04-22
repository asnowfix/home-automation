package ctl

import (
	"context"
	"github.com/asnowfix/home-automation/internal/global"
	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/ctl/blu"
	"github.com/asnowfix/home-automation/myhome/ctl/config"
	"github.com/asnowfix/home-automation/myhome/ctl/db"
	"github.com/asnowfix/home-automation/myhome/ctl/forget"
	"github.com/asnowfix/home-automation/myhome/ctl/heater"
	"github.com/asnowfix/home-automation/myhome/ctl/list"
	"github.com/asnowfix/home-automation/myhome/ctl/mqtt"
	"github.com/asnowfix/home-automation/myhome/ctl/open"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/asnowfix/home-automation/myhome/ctl/pool"
	"github.com/asnowfix/home-automation/myhome/ctl/room"
	"github.com/asnowfix/home-automation/myhome/ctl/sfr"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly"
	"github.com/asnowfix/home-automation/myhome/ctl/show"
	"github.com/asnowfix/home-automation/myhome/ctl/sswitch"
	"github.com/asnowfix/home-automation/myhome/ctl/temperature"
	mqttclient "github.com/asnowfix/home-automation/myhome/mqtt"
	"os"
	"os/signal"
	shellyPkg "github.com/asnowfix/go-shellies"
	shellyscript "github.com/asnowfix/go-shellies/script"
	"github.com/asnowfix/go-shellies/types"
	"github.com/asnowfix/home-automation/internal/shelly/scripts"
	"runtime/pprof"
	"syscall"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:              "ctl",
	Short:            "Control and manage home automation devices",
	Args:             cobra.NoArgs,
	TraverseChildren: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logging based on flags
		verbose := options.Flags.Verbose
		debug := options.Flags.Debug
		hlog.InitWithDebug(verbose, debug)
		log := hlog.Logger
		ctx := logr.NewContext(cmd.Context(), log)

		ctx = options.CommandLineContext(ctx)

		// Set the target instance name for RPC topics
		if options.Flags.InstanceName != "" {
			myhome.InstanceName = options.Flags.InstanceName
		}

		err := mqttclient.NewClientE(ctx, options.Flags.MqttBroker, myhome.InstanceName, options.Flags.MdnsTimeout, options.Flags.MqttTimeout, options.Flags.MqttGrace, options.Flags.MqttReconnectInterval, true)
		if err != nil {
			log.Error(err, "Failed to initialize MQTT client")
			return err
		}

		mc, err := mqttclient.GetClientE(ctx)
		if err != nil {
			log.Error(err, "Failed to start MQTT client")
			return err
		}

		myhome.TheClient, err = myhome.NewClientE(ctx, log, mc, options.Flags.MqttTimeout)
		if err != nil {
			log.Error(err, "Failed to initialize MyHome client")
			return err
		}

		shellyPkg.Init(log, mc, options.Flags.MqttTimeout, options.Flags.ShellyRateLimit, func(log logr.Logger, r types.MethodsRegistrar) {
				shellyscript.Init(log, r, scripts.GetFS())
			})

		// Start cleanup goroutine that closes MQTT client when context is cancelled OR on signal
		// This ensures cleanup happens even when command returns an error or is interrupted
		// (Cobra skips PersistentPostRunE when RunE returns an error)
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGINT)
		go func() {
			select {
			case <-ctx.Done():
				log.V(1).Info("Context cancelled, closing MQTT client")
				mc.Close()
			case sig := <-sigChan:
				log.Info("Signal received, closing MQTT client", "signal", sig)
				mc.Close()
				os.Exit(0)
			}
		}()

		for i, c := range types.Channels {
			if options.Flags.Via == c {
				options.Via = types.Channel(i)
				break
			}
		}

		cmd.SetContext(ctx)

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		f := ctx.Value(global.CpuProfileKey)
		if f != nil {
			defer pprof.StopCPUProfile()
		}

		mc, err := mqttclient.GetClientE(ctx)
		if err != nil {
			return err
		}

		cancel := ctx.Value(global.CancelKey).(context.CancelFunc)
		cancel()
		mc.Close()
		return nil
	},
}

func init() {
	Cmd.PersistentFlags().StringVarP(&options.Flags.CpuProfile, "cpuprofile", "P", "", "write CPU profile to `file`")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.Wait, "wait", "w", options.COMMAND_DEFAULT_TIMEOUT, "Maximum time to wait for command to finish (0 = wait indefinitely)")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Verbose, "verbose", "v", false, "verbose output (info level, mutually exclusive with --debug and --quiet)")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Debug, "debug", "d", false, "debug output (debug level, shows V(1) logs, mutually exclusive with --verbose and --quiet)")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Quiet, "quiet", "q", false, "quiet output (error level only, mutually exclusive with --verbose and --debug)")
	Cmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", options.MQTT_DEFAULT_TIMEOUT, "Timeout for MQTT operations")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttGrace, "mqtt-grace", "G", options.MQTT_DEFAULT_GRACE, "MQTT disconnection grace period")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Json, "json", "j", false, "output in json format")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MdnsTimeout, "mdns-timeout", "M", options.MDNS_LOOKUP_DEFAULT_TIMEOUT, "Timeout for mDNS lookups")
	Cmd.PersistentFlags().StringVarP(&options.Flags.Via, "via", "V", types.ChannelDefault.String(), "Use given channel to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().DurationVar(&options.Flags.ShellyRateLimit, "shelly-rate-limit", options.SHELLY_DEFAULT_RATE_LIMIT, "Minimum interval between commands to the same Shelly device (0 to disable)")
	Cmd.PersistentFlags().StringVarP(&options.Flags.InstanceName, "instance", "I", "myhome", "Target myhome server instance name for RPC (default: myhome)")

	// Make log level flags mutually exclusive
	Cmd.MarkFlagsMutuallyExclusive("verbose", "debug", "quiet")

	Cmd.AddCommand(list.Cmd)
	Cmd.AddCommand(show.Cmd)
	Cmd.AddCommand(open.Cmd)
	Cmd.AddCommand(forget.Cmd)
	Cmd.AddCommand(config.Cmd)
	Cmd.AddCommand(db.Cmd)
	Cmd.AddCommand(mqtt.Cmd)
	Cmd.AddCommand(sswitch.Cmd)
	Cmd.AddCommand(sfr.Cmd)
	Cmd.AddCommand(shelly.Cmd)
	Cmd.AddCommand(blu.Cmd)
	Cmd.AddCommand(temperature.Cmd)
	Cmd.AddCommand(heater.Cmd)
	Cmd.AddCommand(pool.PoolCmd())
	Cmd.AddCommand(room.Cmd)
}

var Commit string
var Version string
