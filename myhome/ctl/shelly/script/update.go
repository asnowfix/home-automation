package script

import (
	"context"
	"fmt"
	"global"
	"hlog"
	mhscript "internal/myhome/shelly/script"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	"pkg/shelly"
	pkgscript "pkg/shelly/script"
	"pkg/shelly/types"
	"reflect"

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
	Use:   "update DEVICE [SCRIPT]",
	Short: "Update all scripts (or a specific script) loaded on the given Shelly device(s) by re-uploading available versions",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		scriptArgs := []string{}
		if len(args) > 1 {
			scriptArgs = []string{args[1]}
		}
		// Script updates can be long: Use a context without timeout
		longCtx := global.ContextWithoutTimeout(cmd.Context(), log)
		_, err := myhome.Foreach(longCtx, log, device, options.Via, doUpdate, scriptArgs)
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

	// Check if a specific script name was provided
	var targetScript string
	if len(args) > 0 {
		targetScript = args[0]
	}

	// Get list of scripts currently loaded on the device
	loaded, err := pkgscript.ListLoaded(ctx, via, sd)
	if err != nil {
		log.Error(err, "Unable to list loaded scripts on device")
		return nil, err
	}

	if targetScript != "" {
		fmt.Printf("Updating %s on %s\n", targetScript, sd.Name())
	} else {
		fmt.Printf("Updating scripts on %s (%d scripts loaded)\n", sd.Name(), len(loaded))
	}

	updateResults := make([]UpdateResult, 0)

	// For each loaded script on the device, try to update it
	for _, loadedScript := range loaded {
		scriptName := loadedScript.Name

		// If a specific script was requested, skip others
		if targetScript != "" && scriptName != targetScript {
			continue
		}

		fmt.Printf("  . Checking %s...\n", scriptName)

		// Try to upload the script (this will check version hash and skip if up-to-date)
		// If the script is not available in embedded scripts, Upload will return an error
		buf, err := pkgscript.ReadEmbeddedFile(scriptName)
		if err != nil {
			fmt.Printf("  ✗ Failed to read script %s: %v\n", scriptName, err)
			updateResults = append(updateResults, UpdateResult{
				ScriptName: scriptName,
				ScriptId:   loadedScript.Id,
				Status:     "error",
				Message:    fmt.Sprintf("Read failed: %v", err),
			})
			continue
		}
		
		id, err := mhscript.UploadWithVersion(ctx, log, via, sd, scriptName, buf, !updateNoMinify, updateForce)
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

	// If a specific script was requested but not found, skip this device silently
	if targetScript != "" && len(updateResults) == 0 {
		fmt.Printf("  → Script %s is not loaded on %s (skipping)\n", targetScript, sd.Name())
		return nil, nil // Return nil error to indicate success (device skipped, not failed)
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
