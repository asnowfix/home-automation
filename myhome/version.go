package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var Program string
var Repo string
var Version string
var Commit string

func init() {
	Cmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Commit)
	},
}
