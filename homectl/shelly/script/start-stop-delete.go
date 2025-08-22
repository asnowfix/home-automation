package script

import (
	"context"
	"fmt"
	"hlog"
	"homectl/options"
	"myhome"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"
	"reflect"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(uploadCtl)
	// Flag to disable minification on upload
	uploadCtl.Flags().BoolVar(&noMinify, "no-minify", false, "Do not minify script before upload")
}

var uploadCtl = &cobra.Command{
	Use:   "upload",
	Short: "Upload a script to the given Shelly device(s)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		scriptName := args[1]
		// minify is true by default unless --no-minify is set
		minify := !noMinify
		// Script upload can be long: Use a long-lived context decoupled from the global command timeout
		longCtx := options.CommandLineContext(context.Background(), hlog.Logger, 2*time.Minute)
		_, err := myhome.Foreach(longCtx, hlog.Logger, device, options.Via, doUpload, []string{scriptName, strconv.FormatBool(minify)})
		return err
	},
}

var noMinify bool

func doUpload(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	scriptName := args[0]
	minify, err := strconv.ParseBool(args[1])
	if err != nil {
		return nil, fmt.Errorf("failed to parse minify argument: %w", err)
	}
	return script.Upload(ctx, via, sd, scriptName, minify)
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

func doStartStopDelete(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	operation := args[0]
	scriptName := args[1]
	out, err := script.StartStopDelete(ctx, via, sd, scriptName, script.Verb(operation))
	if err != nil {
		log.Error(err, "Unable to start/stop/delete script")
		return nil, err
	}
	options.PrintResult(out)
	return out, nil
}
