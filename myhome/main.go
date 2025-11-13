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

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:  "myhome",
	Args: cobra.NoArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// For daemon commands, default to verbose unless --quiet is specified
		isDaemon := cmd.Name() == "daemon" || cmd.Parent() != nil && cmd.Parent().Name() == "daemon"
		verbose := options.Flags.Verbose
		debugFlag := options.Flags.Debug
		if isDaemon {
			// Daemon is verbose (info level) by default unless --quiet is specified
			verbose = !options.Flags.Quiet
		}

		// Initialize logging with debug support
		if isDaemon {
			hlog.InitForDaemonWithDebug(verbose, debugFlag)
		} else {
			hlog.InitWithDebug(verbose, debugFlag)
		}

		log := hlog.Logger
		ctx := logr.NewContext(cmd.Context(), log)

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

		if debug.IsDebuggerAttached() {
			log.Info("Running under debugger (will wait forever)")
			// You can set different timeouts or behavior here
			options.Flags.Wait = 0
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
		if isDaemon {
			options.Flags.Wait = 0
		}

		ctx = options.CommandLineContext(ctx, Version)
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
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Verbose, "verbose", "v", false, "verbose output (info level, mutually exclusive with --debug and --quiet)")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Debug, "debug", "d", false, "debug output (debug level, one level higher than --verbose, mutually exclusive with --verbose and --quiet)")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Quiet, "quiet", "q", false, "quiet output (error level only, mutually exclusive with --verbose and --debug)")
	Cmd.MarkFlagsMutuallyExclusive("verbose", "debug", "quiet")
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
