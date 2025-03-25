package mqtt

import (
	"hlog"
	"pkg/devices"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(subCmd)
}

var Cmd = &cobra.Command{
	Use:   "mqtt",
	Short: "Publish or Subscribe to MQTT topics",
}

var subCmd = &cobra.Command{
	Use:   "sub",
	Short: "Subscribe to device(s) MQTT topic(s)",
	RunE: func(cmd *cobra.Command, args []string) error {
		log := hlog.Logger
		// devices.Init()

		topics, err := devices.Topics(log, args)
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
