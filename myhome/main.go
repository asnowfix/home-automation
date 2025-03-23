package main

import (
	"context"
	"fmt"
	"global"
	"homectl/options"
	"os"

	"hlog"

	"myhome/daemon"

	"debug"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:  "myhome",
	Args: cobra.NoArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		hlog.Init(options.Flags.Verbose)
		if debug.IsDebuggerAttached() {
			hlog.Logger.Info("Running under debugger (will wait forever)")
			// You can set different timeouts or behavior here
			options.Flags.CommandTimeout = 0
		}

		ctx := options.CommandLineContext(hlog.Logger, options.Flags.CommandTimeout)
		cmd.SetContext(ctx)

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cancel := ctx.Value(global.CancelKey).(context.CancelFunc)
		cancel()
		<-ctx.Done()
		return nil
	},
}

func init() {
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Verbose, "verbose", "v", false, "verbose output")
	Cmd.AddCommand(daemon.Cmd)
}

func main() {
	cobra.EnableTraverseRunHooks = true
	err := Cmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
