package follow

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	mhscript "internal/myhome/shelly/script"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	"pkg/shelly"
	pkgscript "pkg/shelly/script"
	"pkg/shelly/types"
	"strconv"
	"strings"
	"time"
	"tools"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var (
	bluFlagAutoOff    int
	bluFlagIllumMin   string
	bluFlagIllumMax   string
	bluFlagSwitchID   string
	bluFlagNextSwitch string
)

// validateIlluminanceValue validates illuminance values (numeric or percentage)
func validateIlluminanceValue(value string) error {
	if value == "" {
		return nil // empty is valid (means not set)
	}
	
	// Check if it's a percentage
	if strings.HasSuffix(value, "%") {
		percentStr := strings.TrimSuffix(value, "%")
		percent, err := strconv.ParseFloat(percentStr, 64)
		if err != nil {
			return fmt.Errorf("invalid percentage value: %q", value)
		}
		if percent < 0 || percent > 100 {
			return fmt.Errorf("percentage must be between 0%% and 100%%, got: %q", value)
		}
		return nil
	}
	
	// Check if it's a numeric value
	_, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("invalid illuminance value: %q (must be numeric or percentage like '20%%')", value)
	}
	return nil
}

// parseIlluminanceValue converts string to appropriate type for JSON
func parseIlluminanceValue(value string) interface{} {
	if value == "" {
		return nil
	}
	
	// If it's a percentage, keep as string
	if strings.HasSuffix(value, "%") {
		return value
	}
	
	// Try to parse as integer first, then float
	if intVal, err := strconv.Atoi(value); err == nil {
		return intVal
	}
	if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
		return floatVal
	}
	
	// Fallback to string (should not happen after validation)
	return value
}

var BluCmd = &cobra.Command{
	Use:   "blu <follower-device> <blu-mac>",
	Short: "Configure Shelly device to follow a Shelly BLU device",
	Long: `Configure Shelly device to follow a Shelly BLU device with illuminance-based triggering.

Illuminance values can be specified as:
- Numeric values (e.g., 10, 50.5) representing lux
- Percentage values (e.g., "20%", "80%") based on 7-day min/max history
  - "0%" = minimum illuminance observed in past 7 days
  - "100%" = maximum illuminance observed in past 7 days
  - "30%" = 30% between observed min and max values

Default illuminance_max is "10%" if not specified.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		followerDevice := args[0]
		mac := tools.NormalizeMac(args[1])
		if mac == "" {
			return fmt.Errorf("invalid BLU MAC address: %q", args[1])
		}

		// Validate illuminance values
		if err := validateIlluminanceValue(bluFlagIllumMin); err != nil {
			return fmt.Errorf("invalid illuminance-min: %w", err)
		}
		if err := validateIlluminanceValue(bluFlagIllumMax); err != nil {
			return fmt.Errorf("invalid illuminance-max: %w", err)
		}

		// Build JSON payload with defaults and optional fields
		payload := make(map[string]any)
		payload["switch_id"] = bluFlagSwitchID
		payload["auto_off"] = bluFlagAutoOff
		
		if cmd.Flags().Changed("illuminance-min") && strings.TrimSpace(bluFlagIllumMin) != "" {
			payload["illuminance_min"] = parseIlluminanceValue(bluFlagIllumMin)
		}
		if cmd.Flags().Changed("illuminance-max") && strings.TrimSpace(bluFlagIllumMax) != "" {
			payload["illuminance_max"] = parseIlluminanceValue(bluFlagIllumMax)
		} else if !cmd.Flags().Changed("illuminance-max") {
			// Set default max to 10% if not explicitly provided
			payload["illuminance_max"] = "10%"
		}
		
		if cmd.Flags().Changed("next-switch") && strings.TrimSpace(bluFlagNextSwitch) != "" {
			payload["next_switch"] = bluFlagNextSwitch
		}

		valueBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		kvKey := "follow/shelly-blu/" + mac

		// Set KVS configuration
		_, err = myhome.Foreach(cmd.Context(), hlog.Logger, followerDevice, options.Via, doSetKVS, []string{kvKey, string(valueBytes)})
		if err != nil {
			return err
		}

		// Upload and start the blu-listener.js script
		fmt.Printf("\nUploading blu-listener.js script...\n")
		longCtx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
		defer cancel()
		_, err = myhome.Foreach(longCtx, hlog.Logger, followerDevice, options.Via, uploadScript, []string{"blu-listener.js"})
		if err != nil {
			return fmt.Errorf("failed to upload script: %w", err)
		}

		return nil
	},
}

func init() {
	// Defaults: auto_off=300s, illuminance_max=10% (percentage-based), switch_id=switch:0
	BluCmd.Flags().IntVar(&bluFlagAutoOff, "auto-off", 300, "Seconds before auto turning off (default 300)")
	BluCmd.Flags().StringVar(&bluFlagIllumMin, "illuminance-min", "", "Minimum illuminance to trigger (numeric lux value or percentage like '20%')")
	BluCmd.Flags().StringVar(&bluFlagIllumMax, "illuminance-max", "", "Maximum illuminance to trigger (numeric lux value or percentage like '80%', default '10%')")
	BluCmd.Flags().StringVar(&bluFlagSwitchID, "switch-id", "switch:0", "Switch ID to operate, e.g. switch:0")
	BluCmd.Flags().StringVar(&bluFlagNextSwitch, "next-switch", "", "Optional next switch ID to turn on after auto-off (unset by default)")
}

// uploadScript is a helper function to upload and start scripts on Shelly devices
func uploadScript(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %T %v", device, device)
	}
	scriptName := args[0]
	fmt.Printf(". Uploading %s to %s...\n", scriptName, sd.Name())
	
	// Read the embedded script file
	buf, err := pkgscript.ReadEmbeddedFile(scriptName)
	if err != nil {
		fmt.Printf("✗ Failed to read script %s: %v\n", scriptName, err)
		return nil, err
	}
	
	// Upload with version tracking (minify=true, force=false)
	id, err := mhscript.UploadWithVersion(ctx, log, via, sd, scriptName, buf, true, false)
	if err != nil {
		fmt.Printf("✗ Failed to upload %s to %s: %v\n", scriptName, sd.Name(), err)
		return nil, err
	}
	fmt.Printf("✓ Successfully uploaded %s to %s (id: %d)\n", scriptName, sd.Name(), id)
	return id, nil
}
