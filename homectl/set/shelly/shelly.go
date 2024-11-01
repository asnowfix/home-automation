package shelly

import (
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(mqttCmd)
}

var Cmd = &cobra.Command{
	Use:   "shelly",
	Short: "Set Shelly devices configuration",
}

var useHttpChannel bool

func init() {
	Cmd.Flags().BoolVarP(&useHttpChannel, "http", "H", false, "Use HTTP channel to communicate with Shelly devices")
}
