package shelly

import (
	"myhome/ctl/shelly/call"
	"myhome/ctl/shelly/components"
	"myhome/ctl/shelly/follow"
	"myhome/ctl/shelly/jobs"
	"myhome/ctl/shelly/kvs"
	"myhome/ctl/shelly/mqtt"
	"myhome/ctl/shelly/reboot"
	"myhome/ctl/shelly/script"
	"myhome/ctl/shelly/setup"
	"myhome/ctl/shelly/sys"
	"myhome/ctl/shelly/wifi"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "shelly",
	Short: "Shelly devices features",
	Args:  cobra.NoArgs,
}

func init() {
	Cmd.AddCommand(call.Cmd)
	Cmd.AddCommand(follow.Cmd)
	Cmd.AddCommand(jobs.Cmd)
	Cmd.AddCommand(mqtt.Cmd)
	Cmd.AddCommand(kvs.Cmd)
	Cmd.AddCommand(script.Cmd)
	Cmd.AddCommand(wifi.Cmd)
	Cmd.AddCommand(sys.Cmd)
	Cmd.AddCommand(components.Cmd)
	Cmd.AddCommand(setup.Cmd)
	Cmd.AddCommand(reboot.Cmd)
}
