package main

import (
	"context"
	"fmt"
	"global"
	"homectl/group"
	"homectl/list"
	"homectl/mqtt"
	"homectl/options"
	"homectl/set"
	"homectl/shelly"
	"homectl/show"
	"homectl/toggle"
	"myhome"
	"mynet"
	"os"
	"time"

	"mymqtt"

	"hlog"

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

		ctx := options.CommandLineContext(log)
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
	Cmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", 7*time.Second, "Timeout for MQTT operations")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttGrace, "mqtt-grace", "G", 500*time.Millisecond, "MQTT disconnection grace period")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Json, "json", "j", false, "output in json format")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MdnsTimeout, "mdns", "M", time.Second*5, "Timeout for mDNS lookups")

	Cmd.AddCommand(versionCmd)
	Cmd.AddCommand(list.Cmd)
	Cmd.AddCommand(show.Cmd)
	Cmd.AddCommand(set.Cmd)
	Cmd.AddCommand(mqtt.Cmd)
	Cmd.AddCommand(toggle.Cmd)
	Cmd.AddCommand(shelly.Cmd)
	Cmd.AddCommand(group.Cmd)
}

var Commit string

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Commit)
	},
}
