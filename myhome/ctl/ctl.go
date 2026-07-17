package ctl

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/ctl/blu"
	"github.com/asnowfix/home-automation/myhome/ctl/config"
	"github.com/asnowfix/home-automation/myhome/ctl/db"
	eventsctl "github.com/asnowfix/home-automation/myhome/ctl/events"
	"github.com/asnowfix/home-automation/myhome/ctl/forget"
	"github.com/asnowfix/home-automation/myhome/ctl/garden"
	"github.com/asnowfix/home-automation/myhome/ctl/heater"
	"github.com/asnowfix/home-automation/myhome/ctl/list"
	ctlmcp "github.com/asnowfix/home-automation/myhome/ctl/mcp"
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
	shellyPkg "github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

// cancelFunc is owned by this package alone, set once in PersistentPreRunE
// and consumed once in PersistentPostRunE. It's a package-level var rather
// than a context value on purpose: context.WithValue is for request-scoped
// data, not for a control-flow handle like a CancelFunc (an unchecked type
// assertion on a missing/mistyped value would panic). ctl.Cmd runs once per
// process invocation, so there's no concurrent-invocation hazard here.
var cancelFunc context.CancelFunc

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

		var cancel context.CancelFunc
		ctx, cancel = options.CommandLineContext(ctx)
		cancelFunc = cancel

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

		shellyPkg.Init(log, mc, options.Flags.MqttTimeout, options.Flags.ShellyRateLimit)

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

		// CPU profile start/stop/close is owned entirely by myhome/main.go's
		// PersistentPostRunE (which always runs, as the root command's
		// persistent hooks apply to every subcommand); nothing to do here.

		mc, err := mqttclient.GetClientE(ctx)
		if err != nil {
			return err
		}

		if cancelFunc != nil {
			cancelFunc()
		}
		mc.Close()
		return nil
	},
}

func init() {
	Cmd.PersistentFlags().StringVarP(&options.Flags.CpuProfile, "cpuprofile", "C", "", "write CPU profile to `file`")
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

	Cmd.AddCommand(ctlmcp.Cmd)
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
	Cmd.AddCommand(garden.GardenCmd())
	Cmd.AddCommand(room.Cmd)
	Cmd.AddCommand(eventsctl.Cmd)
}

var Commit string
var Version string
