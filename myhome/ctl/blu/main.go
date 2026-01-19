package blu

import (
	"myhome/ctl/blu/follow"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "blu",
	Short: "Shelly BLU device features",
	Args:  cobra.NoArgs,
}

func init() {
	Cmd.AddCommand(follow.Cmd)
	Cmd.AddCommand(PublishCmd)
}
