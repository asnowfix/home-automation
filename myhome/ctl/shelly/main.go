// Package shelly groups myhome-business shelly subcommands. Generic Shelly
// device CLI subcommands now live in github.com/asnowfix/go-shellies/cmd/shelly
// and are exposed via the standalone `shelly` binary.
package shelly

import (
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/follow"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/script"
	"github.com/asnowfix/home-automation/myhome/ctl/shelly/setup"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "shelly",
	Short: "Shelly device workflows tied to myhome (script deploy, setup, follow)",
	Args:  cobra.NoArgs,
}

func init() {
	Cmd.AddCommand(follow.Cmd)
	Cmd.AddCommand(script.Cmd)
	Cmd.AddCommand(setup.Cmd)
}
