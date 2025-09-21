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
	"strconv"
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
}

var updateCtl = &cobra.Command{
	Use:   "update DEVICE",
	Short: "Update all scripts loaded on the given Shelly device(s) by re-uploading available versions",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		// minify is true by default unless --no-minify is set
		minify := !updateNoMinify
		// Script updates can be long: Use a long-lived context decoupled from the global command timeout
		longCtx := options.CommandLineContext(context.Background(), log, 5*time.Minute, global.Version(cmd.Context()))
		_, err := myhome.Foreach(longCtx, log, device, options.Via, doUpdate, []string{strconv.FormatBool(minify)})
		return err
	},
}

var updateNoMinify bool

func doUpdate(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	minify, err := strconv.ParseBool(args[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse minify argument: %w", err)
	}

	// Get list of available scripts (embedded in the binary)
	available, err := script.ListAvailable()
	if err != nil {
		log.Error(err, "Unable to list available scripts")
		return nil, err
	}

	// Get list of scripts currently loaded on the device
	loaded, err := script.ListLoaded(ctx, via, sd)
	if err != nil {
		log.Error(err, "Unable to list loaded scripts on device")
		return nil, err
	}

	log.Info("Starting script update process", "device", sd.Name(), "available_scripts", len(available), "loaded_scripts", len(loaded))

	updateResults := make([]UpdateResult, 0)

	// For each loaded script, check if we have an available version to update it with
	for _, loadedScript := range loaded {
		scriptName := loadedScript.Name

		// Check if this script is available in our embedded scripts
		isAvailable := false
		for _, availableScript := range available {
			if availableScript == scriptName {
				isAvailable = true
				break
			}
		}

		if !isAvailable {
			log.Info("Skipping manual script (not available in embedded scripts)", "script", scriptName, "id", loadedScript.Id)
			updateResults = append(updateResults, UpdateResult{
				ScriptName: scriptName,
				ScriptId:   loadedScript.Id,
				Status:     "skipped",
				Message:    "Manual script - not available in embedded scripts",
			})
			continue
		}

		log.Info("Updating script", "script", scriptName, "id", loadedScript.Id)

		// Try to upload the script (this will check version hash and skip if up-to-date)
		id, err := script.Upload(ctx, via, sd, scriptName, minify)
		if err != nil {
			log.Error(err, "Failed to update script", "script", scriptName, "id", loadedScript.Id)
			updateResults = append(updateResults, UpdateResult{
				ScriptName: scriptName,
				ScriptId:   loadedScript.Id,
				Status:     "error",
				Message:    fmt.Sprintf("Upload failed: %v", err),
			})
			continue
		}

		log.Info("Successfully processed script", "script", scriptName, "old_id", loadedScript.Id, "new_id", id)
		updateResults = append(updateResults, UpdateResult{
			ScriptName: scriptName,
			ScriptId:   loadedScript.Id,
			NewId:      id,
			Status:     "updated",
			Message:    "Script processed successfully",
		})
	}

	log.Info("Script update process completed", "device", sd.Name(), "total_processed", len(updateResults))

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
