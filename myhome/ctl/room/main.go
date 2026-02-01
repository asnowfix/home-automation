package room

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "room",
	Short: "Manage device-room associations",
	Long: `Commands for managing device-room associations.

Devices can be assigned to rooms for automatic sensor discovery.
A device can belong to at most one room.`,
}

func init() {
	Cmd.AddCommand(setCmd)
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(clearCmd)
}
