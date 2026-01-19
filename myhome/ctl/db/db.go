package db

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "db",
	Short: "Manage the device database",
	Long: `Commands for managing the myhome device database.

Export, import, and sync device data between myhome instances.`,
}

func init() {
	Cmd.AddCommand(ExportCmd)
	Cmd.AddCommand(ImportCmd)
	Cmd.AddCommand(PullCmd)
}
