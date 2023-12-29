package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var Commit string

func main() {
	Execute()
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use: "homectl",
	// Short: "Show integrated devices.",
	// Run: func(cmd *cobra.Command, args []string) {
	// Do Stuff Here
	// },
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	// Long:  `All software has versions. This is Hugo's`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Commit)
	},
}
