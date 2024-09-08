package main

import (
	"fmt"
	"os"

	"hlog"
	"homectl/list"
	"homectl/mqtt"
	"homectl/set"
	"homectl/show"
	"homectl/toggle"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use: "homectl",
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&hlog.Verbose, "verbose", "v", false, "verbose output")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(list.Cmd)
	rootCmd.AddCommand(show.Cmd)
	rootCmd.AddCommand(set.Cmd)
	rootCmd.AddCommand(mqtt.Cmd)
	rootCmd.AddCommand(toggle.Cmd)
}

var Commit string

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Commit)
	},
}
