package script

import (
	"context"
	"fmt"
	"global"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/script"
	"pkg/shelly/types"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

// Package logger
var log = hlog.GetLogger("myhome/ctl/shelly/script")

func init() {
	Cmd.AddCommand(updateCtl)
	// Flag to disable minification on update
	updateCtl.Flags().BoolVar(&updateNoMinify, "no-minify", false, "Do not minify scripts before upload")
	// Flag to force re-upload even if version hash matches
	updateCtl.Flags().BoolVar(&updateForce, "force", false, "Force re-upload even if version hash matches")
}

var updateCtl = &cobra.Command{
	Use:   "update DEVICE",
	Short: "Update all scripts loaded on the given Shelly device(s) by re-uploading available versions",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		// Script updates can be long: Use a long-lived context decoupled from the global command timeout
		longCtx := options.CommandLineContext(context.Background(), log, 5*time.Minute, global.Version(cmd.Context()))
		_, err := myhome.Foreach(longCtx, log, device, options.Via, doUpdate, []string{})
		return err
	},
}

var updateNoMinify bool
var updateForce bool

func doUpdate(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	// Get list of scripts currently loaded on the device
	loaded, err := script.ListLoaded(ctx, via, sd)
	if err != nil {
		log.Error(err, "Unable to list loaded scripts on device")
		return nil, err
	}

	fmt.Printf("Updating scripts on %s (%d scripts loaded)\n", sd.Name(), len(loaded))

	updateResults := make([]UpdateResult, 0)

	// For each loaded script on the device, try to update it
	for _, loadedScript := range loaded {
		scriptName := loadedScript.Name

		fmt.Printf("  . Checking %s...\n", scriptName)

		// Try to upload the script (this will check version hash and skip if up-to-date)
		// If the script is not available in embedded scripts, Upload will return an error
		id, err := script.Upload(ctx, via, sd, scriptName, !noMinify, forceUpload)
		if err != nil {
			fmt.Printf("  ✗ Failed to update %s: %v\n", scriptName, err)
			updateResults = append(updateResults, UpdateResult{
				ScriptName: scriptName,
				ScriptId:   loadedScript.Id,
				Status:     "error",
				Message:    fmt.Sprintf("Update failed: %v", err),
			})
			continue
		}

		if id == 0 {
			fmt.Printf("  → %s is up-to-date\n", scriptName)
			updateResults = append(updateResults, UpdateResult{
				ScriptName: scriptName,
				ScriptId:   loadedScript.Id,
				Status:     "skipped",
				Message:    "Already up-to-date",
			})
		} else {
			fmt.Printf("  ✓ Successfully updated %s (id: %d)\n", scriptName, id)
			updateResults = append(updateResults, UpdateResult{
				ScriptName: scriptName,
				ScriptId:   loadedScript.Id,
				NewId:      id,
				Status:     "updated",
				Message:    "Script updated successfully",
			})
		}
	}

	fmt.Printf("\nUpdate complete for %s (%d scripts processed)\n", sd.Name(), len(updateResults))

	return UpdateResults{
		Device:  sd.Name(),
		Results: updateResults,
	}, nil
}

// UpdateResult represents the result of updating a single script
type UpdateResult struct {
	ScriptName string `json:"script_name"`
	ScriptId   uint32 `json:"script_id"`
	NewId      uint32 `json:"new_id,omitempty"`
	Status     string `json:"status"` // "updated", "skipped", "error"
	Message    string `json:"message"`
}

// UpdateResults represents the results of updating all scripts on a device
type UpdateResults struct {
	Device  string         `json:"device"`
	Results []UpdateResult `json:"results"`
}
