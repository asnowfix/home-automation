package jobs

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "jobs",
	Short: "Shelly jobs features",
	Args:  cobra.NoArgs,
}

func init() {
	Cmd.AddCommand(scheduleCtl)
	Cmd.AddCommand(showCtl)
	Cmd.AddCommand(cancelCtl)
}
