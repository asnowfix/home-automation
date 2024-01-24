package show

import (
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(showShellyCmd)
	Cmd.AddCommand(showTapoCmd)
}

var Cmd = &cobra.Command{
	Use:   "show",
	Short: "Show devices",
	Run: func(cmd *cobra.Command, args []string) {
	},
}
