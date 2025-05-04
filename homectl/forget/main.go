package forget

import (
	"myhome"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "forget",
	Short: "Delete device(s) from the database",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := "*"
		if len(args) == 1 {
			name = args[0]
		}

		err := myhome.TheClient.ForgetDevices(cmd.Context(), name)
		if err != nil {
			return err
		}
		return nil
	},
}
