package forget

import (
	"github.com/asnowfix/home-automation/internal/myhome"

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

		client, err := myhome.ClientFromContext(cmd.Context())
		if err != nil {
			return err
		}

		err = client.ForgetDevices(cmd.Context(), name)
		if err != nil {
			return err
		}
		return nil
	},
}
