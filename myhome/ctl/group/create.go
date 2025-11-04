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
	Use:   "create <group-name> [key1=val1] [key2=val2] ...",
	Short: "Create group of devices with optional KVS key-value pairs",
	Long: `Create a new device group with an optional set of KVS key-value pairs.

When devices are added to the group, all KVS key-value pairs will be automatically
set on each device. This allows you to configure common settings across all group members.

Examples:
  # Create a simple group
  myhome ctl group create radiateurs

  # Create a group with normally-closed relays
  myhome ctl group create radiateurs normally-closed=true

  # Create a group with multiple KVS pairs
  myhome ctl group create lights auto-off=300 brightness=80`,
	Args: cobra.MinimumNArgs(1),
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
