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
