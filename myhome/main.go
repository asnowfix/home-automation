package main

import (
	"context"
	"fmt"
	"global"
	"homectl/options"
	"os"
	"runtime/pprof"

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
		log := hlog.Logger
		if debug.IsDebuggerAttached() {
			log.Info("Running under debugger (will wait forever)")
			// You can set different timeouts or behavior here
			options.Flags.CommandTimeout = 0
		}

		f, err := os.Create(options.Flags.CpuProfile)
		if err != nil {
			log.Error(err, "Failed to create CPU profile")
			return err
		}
		pprof.StartCPUProfile(f)
		ctx := cmd.Context()
		ctx = context.WithValue(ctx, global.CpuProfileKey, f)
		cmd.SetContext(ctx)

		ctx = options.CommandLineContext(log, options.Flags.CommandTimeout)
		cmd.SetContext(ctx)

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		f := ctx.Value(global.CpuProfileKey)
		if f != nil {
			defer pprof.StopCPUProfile()
		}

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
