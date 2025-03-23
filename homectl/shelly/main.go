package shelly

import (
	jobsCtl "homectl/shelly/jobs"
	kvsCtl "homectl/shelly/kvs"
	mqttCtl "homectl/shelly/mqtt"
	scriptCtl "homectl/shelly/script"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "shelly",
	Short: "Shelly devices features",
	Args:  cobra.NoArgs,
}

func init() {
	Cmd.AddCommand(jobsCtl.Cmd)
	Cmd.AddCommand(mqttCtl.Cmd)
	Cmd.AddCommand(kvsCtl.Cmd)
	Cmd.AddCommand(scriptCtl.Cmd)
}
