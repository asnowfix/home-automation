package main

import (
	"context"
	"fmt"
	"global"
	"homectl/group"
	"homectl/list"
	"homectl/mqtt"
	"homectl/options"
	"homectl/shelly"
	"homectl/show"
	"homectl/toggle"
	"myhome"
	"os"
	"strings"
	"time"

	"mymqtt"

	"hlog"

	"github.com/spf13/cobra"
)

func main() {
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

		ctx, cancel := options.CommandLineContext(log)
		ctx = context.WithValue(ctx, global.CancelKey, cancel)
		cmd.SetContext(ctx)

		options.Devices = strings.Split(options.Flags.Devices, ",")
		log.Info("Will use", "devices", options.Devices)

		var err error
		options.MqttClient, err = mymqtt.InitClientE(cmd.Context(), log, options.Flags.MqttBroker, options.Flags.MqttTimeout, options.Flags.MqttGrace)
		if err != nil {
			log.Error(err, "Failed to initialize MQTT client")
			return err
		}

		options.MyHomeClient, err = myhome.NewClientE(cmd.Context(), log, options.MqttClient, options.Flags.MqttTimeout)
		if err != nil {
			log.Error(err, "Failed to initialize MyHome client")
			return err
		}
		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log := hlog.Logger
		c, err := mymqtt.GetClientE(ctx)
		if err != nil {
			log.Error(err, "Failed to get MQTT client")
			return err
		}
		cancel := ctx.Value(global.CancelKey).(context.CancelFunc)
		cancel()
		c.Close()
		<-ctx.Done()
		// time.Sleep(1 * time.Second)
		hlog.Logger.Info("Finished")
		return nil
	},
}

func init() {
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Verbose, "verbose", "v", false, "verbose output")
	Cmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", 5*time.Second, "Timeout for MQTT operations")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttGrace, "mqtt-grace", "G", 500*time.Millisecond, "MQTT disconnection grace period")
	Cmd.PersistentFlags().StringVarP(&options.Flags.Devices, "devices", "D", "", "comma-separated list of devices to use")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Json, "json", "j", false, "output in json format")

	Cmd.AddCommand(versionCmd)
	Cmd.AddCommand(list.Cmd)
	Cmd.AddCommand(show.Cmd)
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
