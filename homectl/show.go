package main

import (
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(showCmd)
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show devices",
	// Long:  `All software has versions. This is Hugo's`,
}
