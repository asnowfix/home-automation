package main

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"

	"github.com/asnowfix/home-automation/internal/global"
	"github.com/asnowfix/home-automation/myhome/ctl/options"

	"github.com/asnowfix/home-automation/pkg/version"

	"github.com/asnowfix/home-automation/hlog"

	"github.com/asnowfix/home-automation/myhome/ctl"
	"github.com/asnowfix/home-automation/myhome/daemon"

	"github.com/asnowfix/home-automation/internal/debug"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

// cpuProfileFile and shutdownCancel are owned by this package alone: they
// are set once in PersistentPreRunE and consumed once in
// PersistentPostRunE. They intentionally live as package-level vars rather
// than context values — context.WithValue is for request-scoped data, not
// for a control-flow handle (CancelFunc) or a resource that must be closed
// exactly once (*os.File). myhome runs Execute() a single time per process,
// so there's no concurrent-invocation hazard here.
var cpuProfileFile *os.File
var shutdownCancel context.CancelFunc

var Cmd = &cobra.Command{
	Use:  "myhome",
	Args: cobra.NoArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// For daemon commands, default to verbose unless --quiet is specified
		isDaemon := daemon.IsDaemonCommand(cmd)
		verbose := options.Flags.Verbose
		debugFlag := options.Flags.Debug
		if isDaemon {
			// Daemon is verbose (info level) by default unless --quiet is specified
			verbose = !options.Flags.Quiet
		}

		// Activate PanicOnBugs if --panic-on-bugs is specified or --debug is specified
		if options.Flags.Debug {
			global.PanicOnBugs = true
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
			f, err := os.Create(options.Flags.CpuProfile)
			if err != nil {
				log.Error(err, "Failed to create CPU profile")
				return err
			}
			if err := pprof.StartCPUProfile(f); err != nil {
				log.Error(err, "Failed to start CPU profile")
				if closeErr := f.Close(); closeErr != nil {
					log.Error(closeErr, "Failed to close CPU profile file after start failure")
				}
				return err
			}
			cpuProfileFile = f
		}

		if debug.IsDebuggerAttached() {
			log.Info("Running under debugger (will wait forever)")
			// You can set different timeouts or behavior here
			options.Flags.Wait = 0
		}

		// Daemon commands should run indefinitely, no timeout
		if isDaemon {
			options.Flags.Wait = 0
		}

		var cancel context.CancelFunc
		ctx, cancel = options.CommandLineContext(ctx)
		shutdownCancel = cancel
		cmd.SetContext(ctx)

		return nil
	},

	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log := hlog.Logger

		if cpuProfileFile != nil {
			pprof.StopCPUProfile()
			if err := cpuProfileFile.Close(); err != nil {
				log.Error(err, "Failed to close CPU profile file")
			}
			cpuProfileFile = nil
		}

		if shutdownCancel != nil {
			shutdownCancel()
		}

		<-ctx.Done()

		return nil
	},
}

func init() {
	Cmd.PersistentFlags().StringVarP(&options.Flags.CpuProfile, "cpuprofile", "C", "", "write CPU profile to `file`")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Verbose, "verbose", "v", false, "verbose output (info level, mutually exclusive with --debug and --quiet)")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Debug, "debug", "d", false, "debug output (debug level, one level higher than --verbose, mutually exclusive with --verbose and --quiet)")
	Cmd.PersistentFlags().BoolVarP(&options.Flags.Quiet, "quiet", "q", false, "quiet output (error level only, mutually exclusive with --verbose and --debug)")
	Cmd.MarkFlagsMutuallyExclusive("verbose", "debug", "quiet")
	Cmd.AddCommand(daemon.Cmd)
	Cmd.AddCommand(ctl.Cmd)
	Cmd.AddCommand(version.Cmd)
}

func main() {
	cobra.EnableTraverseRunHooks = true
	err := Cmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
