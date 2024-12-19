package main

import (
	"fmt"
	"homectl/list"
	"homectl/mqtt"
	shellyCtl "homectl/shelly"
	"homectl/show"
	"homectl/toggle"
	"os"

	"hlog"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var logger logr.Logger

var Cmd = &cobra.Command{
	Use: "homectl",
	Run: func(cmd *cobra.Command, args []string) {
		logger = hlog.Init()
	},
}

func init() {
	Cmd.PersistentFlags().BoolVarP(&hlog.Verbose, "verbose", "v", false, "verbose output")
	Cmd.AddCommand(versionCmd)
	Cmd.AddCommand(list.Cmd)
	Cmd.AddCommand(show.Cmd)
	Cmd.AddCommand(mqtt.Cmd)
	Cmd.AddCommand(toggle.Cmd)
	Cmd.AddCommand(shellyCtl.Cmd)
}

var Commit string

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Commit)
	},
}
