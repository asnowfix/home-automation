package main

import (
	"context"
	"fmt"
	"global"
	"myhome/ctl/options"
	"os"
	"runtime/pprof"

	"hlog"

	"myhome/ctl"
	"myhome/daemon"

	"debug"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:  "myhome",
	Args: cobra.NoArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// For daemon commands, default to verbose unless --quiet is specified
		isDaemon := cmd.Name() == "daemon" || cmd.Parent() != nil && cmd.Parent().Name() == "daemon"
		verbose := options.Flags.Verbose
		if isDaemon {
			verbose = !options.Flags.Quiet // daemon is verbose by default unless --quiet
		}

		// Use InitForDaemon for daemon commands to default to info level
		if isDaemon {
			hlog.InitForDaemon(verbose)
		} else {
			hlog.Init(verbose)
		}

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

		// Daemon commands should run indefinitely, no timeout
		timeout := options.Flags.CommandTimeout
		if isDaemon {
			timeout = 0
		}

		ctx = options.CommandLineContext(ctx, log, timeout, Version)
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
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Quiet, "quiet", "q", false, "quiet output (suppress info logs)")
	Cmd.AddCommand(daemon.Cmd)
	Cmd.AddCommand(ctl.Cmd)
}

func main() {
	cobra.EnableTraverseRunHooks = true
	err := Cmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
