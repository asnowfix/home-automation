package mqtt

import (
	"devices"
	"hlog"
	"homectl/options"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(pubCmd)
	Cmd.AddCommand(subCmd)
}

var Cmd = &cobra.Command{
	Use:   "mqtt",
	Short: "Publish or Subscribe to MQTT topics",
}

var pubCmd = &cobra.Command{
	Use:   "pub",
	Short: "Publish to device(s) MQTT topic(s)",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		// devices.Init()

		log.Info("looking for devices: %v", options.Devices)
		topics, err := devices.Topics(log, options.Devices)
		if err != nil {
			log.Error(err, "Failed to list devices")
			return err
		}
		msg := strings.Join(args, "")
		for _, topic := range topics {
			log.Info("MQTT <<< %v", msg)
			topic.Publish([]byte(msg))
		}
		return nil
	},
}

var subCmd = &cobra.Command{
	Use:   "sub",
	Short: "Subscribe to device(s) MQTT topic(s)",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		// devices.Init()

		topics, err := devices.Topics(log, options.Devices)
		if err != nil {
			log.Error(err, "Failed to list devices")
			return err
		}
		for _, topic := range topics {
			topic.Subscribe(func(msg []byte) {
				log.Info("MQTT >>> %v", msg)
			})
		}
		return nil
	},
}
