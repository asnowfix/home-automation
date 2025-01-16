package main

import (
	"fmt"
	"os"

	"hlog"

	"myhome/daemon"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var logger logr.Logger

var Cmd = &cobra.Command{
	Use:  "myhome",
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		logger = hlog.Init()
	},
}

func init() {
	Cmd.PersistentFlags().BoolVarP(&hlog.Verbose, "verbose", "v", false, "verbose output")
	Cmd.AddCommand(daemon.Cmd)
}

func main() {
	// cobra main parser
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
