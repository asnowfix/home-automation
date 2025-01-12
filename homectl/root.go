package main

import (
	"fmt"
	"homectl/list"
	"homectl/mqtt"
	"homectl/options"
	"homectl/shelly"
	"homectl/show"
	"homectl/toggle"
	"os"
	"strings"

	"mymqtt"

	"hlog"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var log logr.Logger

var Cmd = &cobra.Command{
	Use:  "homectl",
	Args: cobra.NoArgs,
	// run for this command and any sub-command
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		log = hlog.Init()
		options.Devices = strings.Split(options.Flags.Devices, ",")
		log.Info("Will use", "devices", options.Devices)

		var err error
		options.MqttClient, err = mymqtt.NewClientE(log, options.Flags.MqttBroker)
		if err != nil {
			log.Error(err, "Failed to create MQTT client")
			os.Exit(1)
		}
	},
}

func init() {
	Cmd.PersistentFlags().BoolVarP(&hlog.Verbose, "verbose", "v", false, "verbose output")
	Cmd.PersistentFlags().StringVarP(&options.Flags.MqttBroker, "mqtt-broker", "B", "", "Use given MQTT broker URL to communicate with Shelly devices (default is to discover it from the network)")
	Cmd.PersistentFlags().StringVarP(&options.Flags.Devices, "devices", "D", "", "comma-separated list of devices to use")

	Cmd.AddCommand(versionCmd)
	Cmd.AddCommand(list.Cmd)
	Cmd.AddCommand(show.Cmd)
	Cmd.AddCommand(mqtt.Cmd)
	Cmd.AddCommand(toggle.Cmd)
	Cmd.AddCommand(shelly.Cmd)
}

var Commit string

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Commit)
	},
}
