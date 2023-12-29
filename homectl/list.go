package main

import (
	"devices/sfr"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List known devices connected on the home gateway",
	Run: func(cmd *cobra.Command, args []string) {
		sfr.ListDevices()
	},
}
