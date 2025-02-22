package group

import (
	"homectl/options"
	"myhome"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(createCmd)
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create device groups",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		var description string
		if len(args) > 1 {
			description = args[1]
		}
		_, err := options.MyHomeClient.CallE(cmd.Context(), myhome.GroupCreate, &myhome.GroupInfo{Name: name, Description: description})
		return err
	},
}
