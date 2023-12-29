package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	showCmd.AddCommand(tapoCmd)
}

var tapoCmd = &cobra.Command{
	Use:   "tapo",
	Short: "Show Tapo devices",
	// Long:  `All software has versions. This is Hugo's`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Hugo Static Site Generator v0.9 -- HEAD")
	},
}
