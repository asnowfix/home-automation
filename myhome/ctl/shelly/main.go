package shelly

import (
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/call"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/components"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/follow"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/jobs"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/kvs"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/mqtt"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/reboot"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/script"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/setup"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/status"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/sys"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/wifi"

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
	Cmd.AddCommand(status.Cmd)
}
