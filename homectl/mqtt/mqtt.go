package mqtt

import (
	"devices"
	"hlog"
	"strings"

	"github.com/spf13/cobra"
)

var options struct {
	devices string
}

func init() {
	Cmd.Flags().StringVarP(&options.devices, "devices", "D", "", "comma-separated list of MQTT devices to send the message to")

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
	Run: func(cmd *cobra.Command, args []string) {
		log := hlog.Init()
		// devices.Init()

		dn := strings.Split(options.devices, ",")
		log.Info("looking for devices: %v", dn)
		topics, err := devices.Topics(log, dn)
		if err != nil {
			log.Error(err, "Failed to list devices")
		}
		msg := strings.Join(args, "")
		for _, topic := range topics {
			log.Info("MQTT <<< %v", msg)
			topic.Publish([]byte(msg))
		}
	},
}

var subCmd = &cobra.Command{
	Use:   "sub",
	Short: "Subscribe to device(s) MQTT topic(s)",
	Run: func(cmd *cobra.Command, args []string) {
		log := hlog.Init()
		// devices.Init()

		topics, err := devices.Topics(log, strings.Split(options.devices, ","))
		if err != nil {
			log.Error(err, "Failed to list devices")
		}
		for _, topic := range topics {
			topic.Subscribe(func(msg []byte) {
				log.Info("MQTT >>> %v", msg)
			})
		}
	},
}
