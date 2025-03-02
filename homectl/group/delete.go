package group

import (
	"myhome"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(deleteCmd)
}

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete device groups",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		_, err := myhome.TheClient.CallE(cmd.Context(), myhome.GroupDelete, name)
		return err
	},
}
