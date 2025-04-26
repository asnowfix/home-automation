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
	"mynet"
	"os"
	shellyPkg "pkg/shelly"
	"pkg/shelly/types"
	"time"

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

		if debug.IsDebuggerAttached() {
			hlog.Logger.Info("Running under debugger (will wait forever)")
			// You can set different timeouts or behavior here
			options.Flags.CommandTimeout = 0
		}

		ctx := options.CommandLineContext(log, options.Flags.CommandTimeout)
		cmd.SetContext(ctx)

		mc, err := mymqtt.InitClientE(ctx, log, mynet.MyResolver(log).Start(ctx), options.Flags.MqttBroker, options.Flags.MqttTimeout, options.Flags.MqttGrace, options.Flags.MdnsTimeout)
		if err != nil {
			log.Error(err, "Failed to initialize MQTT client")
			return err
		}

		myhome.TheClient, err = myhome.NewClientE(cmd.Context(), log, mc, options.Flags.MqttTimeout)
		if err != nil {
			log.Error(err, "Failed to initialize MyHome client")
			return err
		}

		shellyPkg.Init(cmd.Context(), options.Flags.MqttTimeout)

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
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Verbose, "verbose", "v", false, "verbose output")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.CommandTimeout, "timeout", "", 7*time.Second, "Timeout for overall command")
	Cmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", 6*time.Second, "Timeout for MQTT operations")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttGrace, "mqtt-grace", "G", 500*time.Millisecond, "MQTT disconnection grace period")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Json, "json", "j", false, "output in json format")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MdnsTimeout, "mdns-timeout", "M", time.Second*5, "Timeout for mDNS lookups")
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
