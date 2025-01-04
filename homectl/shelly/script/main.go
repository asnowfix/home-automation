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
	Id   uint32
	Name string
}

func init() {
	Cmd.PersistentFlags().Uint32VarP(&flags.Id, "id", "i", 0, "Script Id")
	Cmd.PersistentFlags().StringVarP(&flags.Name, "name", "n", "", "Script Name")
	Cmd.MarkFlagsMutuallyExclusive("id", "name")
	// Cmd.MarkFlagsOneRequired("id", "name", "all")
}
