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
		ctx := cmd.Context()

		if debug.IsDebuggerAttached() {
			log.Info("Running under debugger (will wait forever)")
			// You can set different timeouts or behavior here
			options.Flags.CommandTimeout = 0
		}

		if options.Flags.CpuProfile != "" {
			if options.Flags.CpuProfile != "" {
				f, err := os.Create(options.Flags.CpuProfile)
				if err != nil {
					log.Error(err, "Failed to create CPU profile")
					return err
				}
				pprof.StartCPUProfile(f)
				ctx = context.WithValue(ctx, global.CpuProfileKey, f)
			}
		}

		ctx = options.CommandLineContext(ctx, log, options.Flags.CommandTimeout, Version)
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
	Cmd.PersistentFlags().StringVarP(&options.Flags.CpuProfile, "cpuprofile", "P", "", "write CPU profile to `file`")
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
