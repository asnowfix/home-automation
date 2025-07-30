package main

import (
	"context"
	"fmt"
	"global"
	"homectl/follow"
	"homectl/forget"
	"homectl/group"
	"homectl/list"
	"homectl/mqtt"
	"homectl/open"
	"homectl/options"
	"homectl/set"
	"homectl/shelly"
	"homectl/show"
	"homectl/sswitch"
	"myhome"
	"os"
	shellyPkg "pkg/shelly"
	"pkg/shelly/types"
	"runtime/pprof"

	"mymqtt"

	"hlog"

	"debug"

	"github.com/spf13/cobra"
)

func main() {
	cobra.EnableTraverseRunHooks = true
	err := Cmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var Cmd = &cobra.Command{
	Use:  "homectl",
	Args: cobra.NoArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		hlog.Init(options.Flags.Verbose)
		log := hlog.Logger
		ctx := cmd.Context()

		if debug.IsDebuggerAttached() {
			hlog.Logger.Info("Running under debugger (will wait forever)")
			options.Flags.CommandTimeout = 0
		}

		if options.Flags.CpuProfile != "" {
			if options.Flags.CpuProfile != "" {
				f, err := os.Create(options.Flags.CpuProfile)
				if err != nil {
					log.Error(err, "Failed to create CPU profile")
					return err
				}
				pprof.StartCPUProfile(f)
				ctx = context.WithValue(ctx, global.CpuProfileKey, f)
			}
		}

		ctx = options.CommandLineContext(ctx, log, options.Flags.CommandTimeout)
		cmd.SetContext(ctx)

		err := mymqtt.NewClientE(ctx, log, options.Flags.MqttBroker, options.Flags.MdnsTimeout, options.Flags.MqttTimeout, options.Flags.MqttGrace)
		if err != nil {
			log.Error(err, "Failed to initialize MQTT client")
			return err
		}

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

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		f := ctx.Value(global.CpuProfileKey)
		if f != nil {
			defer pprof.StopCPUProfile()
		}

		mc, err := mymqtt.GetClientE(ctx)
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
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Verbose, "verbose", "v", false, "verbose output")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.CommandTimeout, "timeout", "", options.COMMAND_TIMEOUT, "Timeout for overall command")
	Cmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", options.MQTT_DEFAULT_TIMEOUT, "Timeout for MQTT operations")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttGrace, "mqtt-grace", "G", options.MQTT_DEFAULT_GRACE, "MQTT disconnection grace period")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Json, "json", "j", false, "output in json format")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MdnsTimeout, "mdns-timeout", "M", options.MDNS_LOOKUP_TIMEOUT, "Timeout for mDNS lookups")
	Cmd.PersistentFlags().StringVarP(&options.Flags.Via, "via", "V", types.ChannelDefault.String(), "Use given channel to communicate with Shelly devices (default is to discover it from the network)")

	Cmd.AddCommand(versionCmd)
	Cmd.AddCommand(list.Cmd)
	Cmd.AddCommand(set.Cmd)
	Cmd.AddCommand(show.Cmd)
	Cmd.AddCommand(open.Cmd)
	Cmd.AddCommand(forget.Cmd)
	Cmd.AddCommand(mqtt.Cmd)
	Cmd.AddCommand(sswitch.Cmd)
	Cmd.AddCommand(shelly.Cmd)
	Cmd.AddCommand(group.Cmd)
	Cmd.AddCommand(follow.FollowCmd)
	Cmd.AddCommand(follow.UnfollowCmd)
}

var Commit string

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Commit)
	},
}
