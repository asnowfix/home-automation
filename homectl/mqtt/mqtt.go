package mqtt

import (
	"fmt"

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
		return fmt.Errorf("not implemented")
	},
}
