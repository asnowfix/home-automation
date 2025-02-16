package group

import (
	"homectl/options"

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
		ctx, cancel := options.InterruptibleContext()
		defer cancel()
		_, err := options.MyHomeClient.CallE(ctx, "group.delete", name)
		return err
	},
}
