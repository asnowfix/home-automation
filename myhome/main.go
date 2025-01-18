package main

import (
	"fmt"
	"os"

	"hlog"

	"myhome/daemon"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:  "myhome",
	Args: cobra.NoArgs,
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
