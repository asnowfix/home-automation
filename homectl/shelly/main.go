package shelly

import (
	jobsCtl "homectl/shelly/jobs"
	kvsCtl "homectl/shelly/kvs"
	mqttCtl "homectl/shelly/mqtt"
	"homectl/shelly/options"
	scriptCtl "homectl/shelly/script"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "shelly",
	Short: "Shelly devices features",
	Args:  cobra.NoArgs,
}

func init() {
	Cmd.PersistentFlags().BoolVarP(&options.UseHttpChannel, "http", "H", false, "Use HTTP channel to communicate with Shelly devices")

	Cmd.AddCommand(jobsCtl.Cmd)
	Cmd.AddCommand(mqttCtl.Cmd)
	Cmd.AddCommand(kvsCtl.Cmd)
	Cmd.AddCommand(scriptCtl.Cmd)
}
