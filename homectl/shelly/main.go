package shelly

import (
	compsCtl "homectl/shelly/components"
	jobsCtl "homectl/shelly/jobs"
	kvsCtl "homectl/shelly/kvs"
	mqttCtl "homectl/shelly/mqtt"
	scriptCtl "homectl/shelly/script"
	setupCtl "homectl/shelly/setup"
	sysCtl "homectl/shelly/sys"
	wifiCtl "homectl/shelly/wifi"

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
	Cmd.AddCommand(wifiCtl.Cmd)
	Cmd.AddCommand(sysCtl.Cmd)
	Cmd.AddCommand(compsCtl.Cmd)
	Cmd.AddCommand(setupCtl.Cmd)
}
