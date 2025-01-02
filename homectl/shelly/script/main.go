package script

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "script",
	Short: "Manage scripts running on Shelly devices",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
	},
}

var flags struct {
	Id int
}

func init() {
	Cmd.PersistentFlags().IntVarP(&flags.Id, "id", "i", 0, "Script ID")
}
