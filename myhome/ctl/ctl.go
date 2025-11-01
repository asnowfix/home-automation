package ctl

import (
	"context"
	"global"
	"hlog"
	"myhome"
	"myhome/ctl/follow"
	"myhome/ctl/forget"
	"myhome/ctl/group"
	"myhome/ctl/list"
	"myhome/ctl/mqtt"
	"myhome/ctl/open"
	"myhome/ctl/options"
	"myhome/ctl/sfr"
	"myhome/ctl/shelly"
	"myhome/ctl/show"
	"myhome/ctl/sswitch"
	mqttclient "myhome/mqtt"
	shellyPkg "pkg/shelly"
	shellyMqtt "pkg/shelly/mqtt"
	"pkg/shelly/types"
	"runtime/pprof"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "ctl",
	Short: "Control and manage home automation devices",
	Args:  cobra.NoArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// For ctl commands, default to quiet (error level) unless --verbose is specified
		verbose := options.Flags.Verbose && !options.Flags.Quiet
		hlog.Init(verbose)
		log := hlog.Logger
		ctx := logr.NewContext(cmd.Context(), log)

		ctx = options.CommandLineContext(ctx, Version)

		err := mqttclient.NewClientE(ctx, options.Flags.MqttBroker, options.Flags.MdnsTimeout, options.Flags.MqttTimeout, options.Flags.MqttGrace)
		if err != nil {
			log.Error(err, "Failed to initialize MQTT client")
			return err
		}

		mc, err := mqttclient.GetClientE(ctx)
		if err != nil {
			log.Error(err, "Failed to get MQTT client")
			return err
		}

		// Add Shelly MQTT client to context
		ctx = shellyMqtt.NewContext(ctx, mc)

		myhome.TheClient, err = myhome.NewClientE(ctx, log, options.Flags.MqttTimeout)
		if err != nil {
			log.Error(err, "Failed to initialize MyHome client")
			return err
		}

		shellyPkg.Init(log, options.Flags.MqttTimeout)

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
		<-ctx.Done()
		return nil
	},
}

func init() {
	Cmd.PersistentFlags().StringVarP(&options.Flags.CpuProfile, "cpuprofile", "P", "", "write CPU profile to `file`")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.Wait, "wait", "w", options.COMMAND_DEFAULT_TIMEOUT, "Maximum time to wait for command to finish (0 = wait indefinitely)")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Verbose, "verbose", "v", false, "verbose output")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Quiet, "quiet", "q", false, "quiet output (suppress info logs)")
	Cmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", options.MQTT_DEFAULT_TIMEOUT, "Timeout for MQTT operations")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttGrace, "mqtt-grace", "G", options.MQTT_DEFAULT_GRACE, "MQTT disconnection grace period")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Json, "json", "j", false, "output in json format")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MdnsTimeout, "mdns-timeout", "M", options.MDNS_LOOKUP_DEFAULT_TIMEOUT, "Timeout for mDNS lookups")
	Cmd.PersistentFlags().StringVarP(&options.Flags.Via, "via", "V", types.ChannelDefault.String(), "Use given channel to communicate with Shelly devices (default is to discover it from the network)")

	Cmd.AddCommand(list.Cmd)
	Cmd.AddCommand(show.Cmd)
	Cmd.AddCommand(open.Cmd)
	Cmd.AddCommand(forget.Cmd)
	Cmd.AddCommand(mqtt.Cmd)
	Cmd.AddCommand(sswitch.Cmd)
	Cmd.AddCommand(sfr.Cmd)
	Cmd.AddCommand(shelly.Cmd)
	Cmd.AddCommand(group.Cmd)
	Cmd.AddCommand(follow.FollowCmd)
	Cmd.AddCommand(follow.UnfollowCmd)
}

var Commit string
var Version string
