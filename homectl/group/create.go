package group

import (
	"fmt"
	"myhome"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(createCmd)
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create group of devices",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		gi := &myhome.GroupInfo{Name: name}
		if len(args) > 1 {
			for _, arg := range args[1:] {
				kv := strings.Split(arg, "=")
				if len(kv) != 2 {
					return fmt.Errorf("invalid key-value pair: %s", arg)
				}
				gi.WithKeyValue(kv[0], kv[1])
			}
		}
		_, err := myhome.TheClient.CallE(cmd.Context(), myhome.GroupCreate, gi)
		return err
	},
}
