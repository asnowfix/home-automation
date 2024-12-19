package shelly

import (
	jobsCtl "homectl/shelly/jobs"
	mqttCtl "homectl/shelly/mqtt"
	"homectl/shelly/options"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "shelly",
	Short: "Shelly devices features",
	Run: func(cmd *cobra.Command, args []string) {
	},
}

func init() {
	Cmd.PersistentFlags().BoolVarP(&options.UseHttpChannel, "http", "H", false, "Use HTTP channel to communicate with Shelly devices")
	Cmd.AddCommand(jobsCtl.Cmd)
	Cmd.AddCommand(mqttCtl.Cmd)
}
