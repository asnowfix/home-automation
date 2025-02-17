package main

import (
	"fmt"
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
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var Cmd = &cobra.Command{
	Use:  "homectl",
	Args: cobra.NoArgs,
	// run for this command and any sub-command
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		hlog.Init(options.Flags.Verbose)
		log := hlog.Logger
		options.Devices = strings.Split(options.Flags.Devices, ",")
		log.Info("Will use", "devices", options.Devices)

		ctx := options.CommandLineContext()
		var err error
		options.MqttClient, err = mymqtt.InitClientE(ctx, log, options.Flags.MqttBroker, "", options.Flags.MqttTimeout)
		if err != nil {
			log.Error(err, "Failed to initialize MQTT client")
			return err
		}
		options.MyHomeClient, err = myhome.NewClientE(ctx, log, options.MqttClient, options.Flags.MqttTimeout)
		if err != nil {
			log.Error(err, "Failed to initialize MyHome client")
			return err
		}
		return nil
	},
}

func init() {
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Verbose, "verbose", "v", false, "verbose output")
	Cmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().DurationVarP(&options.Flags.MqttTimeout, "mqtt-timeout", "T", 5*time.Second, "Timeout for MQTT operations")
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
