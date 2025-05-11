package script

import (
	"context"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(uploadCtl)
}

var uploadCtl = &cobra.Command{
	Use:   "upload",
	Short: "Upload a script to the given Shelly device(s)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		scriptName := args[1]
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doUpload, []string{scriptName})
		return err
	},
}

func doUpload(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	scriptName := args[0]
	return script.Upload(ctx, via, device, scriptName)
}

func init() {
	Cmd.AddCommand(startCtl)
}

var startCtl = &cobra.Command{
	Use:   "start",
	Short: "Start a script on the given Shelly device(s)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		scriptName := args[1]
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doStartStopDelete, []string{script.Start.String(), scriptName})
		return err
	},
}

func init() {
	Cmd.AddCommand(stopCtl)
}

var stopCtl = &cobra.Command{
	Use:   "stop",
	Short: "Stop a script on the given Shelly device(s)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		scriptName := args[1]
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doStartStopDelete, []string{script.Stop.String(), scriptName})
		return err
	},
}

func init() {
	Cmd.AddCommand(deleteCtl)
}

var deleteCtl = &cobra.Command{
	Use:   "delete",
	Short: "Delete a script loaded on the given Shelly device(s)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		scriptName := args[1]
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doStartStopDelete, []string{script.Delete.String(), scriptName})
		return err
	},
}

func doStartStopDelete(ctx context.Context, log logr.Logger, via types.Channel, device *shelly.Device, args []string) (any, error) {
	operation := args[0]
	scriptName := args[1]
	out, err := script.StartStopDelete(ctx, via, device, scriptName, script.Verb(operation))
	if err != nil {
		log.Error(err, "Unable to start/stop/delete script")
		return nil, err
	}
	options.PrintResult(out)
	return out, nil
}
