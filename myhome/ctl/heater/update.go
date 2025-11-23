package heater

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
	"pkg/shelly/kvs"
	pkgscript "pkg/shelly/script"
	"pkg/shelly/types"
	"reflect"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var updateFlags struct {
	// KVS configuration flags (all optional for update)
	EnableLogging            *bool
	SetPoint                 *float64
	MinInternalTemp          *float64
	CheapStartHour           *int
	CheapEndHour             *int
	PollIntervalMs           *int
	PreheatHours             *int
	NormallyClosed           *bool
	InternalTemperatureTopic *string
	ExternalTemperatureTopic *string
	RoomId                   *string

	// Script upload flags
	NoMinify    bool
	ForceUpload bool
}

var updateCmd = &cobra.Command{
	Use:   "update <device>",
	Short: "Update heater script configuration on a Shelly device",
	Long: `Update specific KVS configuration values and/or the heater.js script.

Only the configuration values provided as flags will be updated. Existing values are preserved.
The script will be updated if a new version is available (or if --force is used).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doUpdate, nil)
		return err
	},
}

func init() {
	// KVS configuration flags - all optional (use pointers to detect if set)
	var enableLogging bool
	var setPoint float64
	var minInternalTemp float64
	var cheapStartHour int
	var cheapEndHour int
	var pollIntervalMs int
	var preheatHours int
	var normallyClosed bool
	var internalTemperatureTopic string
	var externalTemperatureTopic string
	var roomId string

	updateCmd.Flags().BoolVar(&enableLogging, "enable-logging", false, "Enable logging")
	updateCmd.Flags().Float64Var(&setPoint, "set-point", 0, "Target temperature")
	updateCmd.Flags().Float64Var(&minInternalTemp, "min-internal-temp", 0, "Minimum internal temperature threshold")
	updateCmd.Flags().IntVar(&cheapStartHour, "cheap-start-hour", 0, "Start hour of cheap electricity window")
	updateCmd.Flags().IntVar(&cheapEndHour, "cheap-end-hour", 0, "End hour of cheap electricity window")
	updateCmd.Flags().IntVar(&pollIntervalMs, "poll-interval-ms", 0, "Polling interval in milliseconds")
	updateCmd.Flags().IntVar(&preheatHours, "preheat-hours", 0, "Hours before cheap window end to start preheating")
	updateCmd.Flags().BoolVar(&normallyClosed, "normally-closed", false, "Whether the switch is normally closed")
	updateCmd.Flags().StringVar(&internalTemperatureTopic, "internal-temperature-topic", "", "MQTT topic for internal temperature sensor")
	updateCmd.Flags().StringVar(&externalTemperatureTopic, "external-temperature-topic", "", "MQTT topic for external temperature sensor")
	updateCmd.Flags().StringVar(&roomId, "room-id", "", "Room identifier for temperature API")

	// Script upload flags
	updateCmd.Flags().BoolVar(&updateFlags.NoMinify, "no-minify", false, "Do not minify script before upload")
	updateCmd.Flags().BoolVar(&updateFlags.ForceUpload, "force", false, "Force re-upload even if version hash matches")

	// Set up pointers after flag parsing
	updateCmd.PreRun = func(cmd *cobra.Command, args []string) {
		if cmd.Flags().Changed("enable-logging") {
			updateFlags.EnableLogging = &enableLogging
		}
		if cmd.Flags().Changed("set-point") {
			updateFlags.SetPoint = &setPoint
		}
		if cmd.Flags().Changed("min-internal-temp") {
			updateFlags.MinInternalTemp = &minInternalTemp
		}
		if cmd.Flags().Changed("cheap-start-hour") {
			updateFlags.CheapStartHour = &cheapStartHour
		}
		if cmd.Flags().Changed("cheap-end-hour") {
			updateFlags.CheapEndHour = &cheapEndHour
		}
		if cmd.Flags().Changed("poll-interval-ms") {
			updateFlags.PollIntervalMs = &pollIntervalMs
		}
		if cmd.Flags().Changed("preheat-hours") {
			updateFlags.PreheatHours = &preheatHours
		}
		if cmd.Flags().Changed("normally-closed") {
			updateFlags.NormallyClosed = &normallyClosed
		}
		if cmd.Flags().Changed("internal-temperature-topic") {
			updateFlags.InternalTemperatureTopic = &internalTemperatureTopic
		}
		if cmd.Flags().Changed("external-temperature-topic") {
			updateFlags.ExternalTemperatureTopic = &externalTemperatureTopic
		}
		if cmd.Flags().Changed("room-id") {
			updateFlags.RoomId = &roomId
		}
	}
}

func doUpdate(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	fmt.Printf("Updating heater configuration on %s...\n", sd.Name())

	// Fetch current KVS values
	fmt.Printf("\nFetching current configuration...\n")
	currentConfig := make(map[string]string)
	for _, key := range heaterKVSKeys {
		value, err := kvs.GetValue(ctx, log, via, sd, key)
		if err == nil && value != nil && value.Value != "" {
			currentConfig[key] = value.Value
		}
	}

	// Build update map with only changed values
	updatesToApply := make(map[string]interface{})

	if updateFlags.EnableLogging != nil {
		updatesToApply["script/heater/enable-logging"] = *updateFlags.EnableLogging
	}
	if updateFlags.SetPoint != nil {
		updatesToApply["script/heater/set-point"] = *updateFlags.SetPoint
	}
	if updateFlags.MinInternalTemp != nil {
		updatesToApply["script/heater/min-internal-temp"] = *updateFlags.MinInternalTemp
	}
	if updateFlags.CheapStartHour != nil {
		updatesToApply["script/heater/cheap-start-hour"] = *updateFlags.CheapStartHour
	}
	if updateFlags.CheapEndHour != nil {
		updatesToApply["script/heater/cheap-end-hour"] = *updateFlags.CheapEndHour
	}
	if updateFlags.PollIntervalMs != nil {
		updatesToApply["script/heater/poll-interval-ms"] = *updateFlags.PollIntervalMs
	}
	if updateFlags.PreheatHours != nil {
		updatesToApply["script/heater/preheat-hours"] = *updateFlags.PreheatHours
	}
	if updateFlags.NormallyClosed != nil {
		updatesToApply["normally-closed"] = *updateFlags.NormallyClosed
	}
	if updateFlags.InternalTemperatureTopic != nil {
		updatesToApply["script/heater/internal-temperature-topic"] = *updateFlags.InternalTemperatureTopic
	}
	if updateFlags.ExternalTemperatureTopic != nil {
		updatesToApply["script/heater/external-temperature-topic"] = *updateFlags.ExternalTemperatureTopic
	}
	if updateFlags.RoomId != nil {
		updatesToApply["script/heater/room-id"] = *updateFlags.RoomId
	}

	// Apply KVS updates
	if len(updatesToApply) > 0 {
		fmt.Printf("\nUpdating KVS entries:\n")
		for key, value := range updatesToApply {
			var valueStr string
			switch v := value.(type) {
			case string:
				valueStr = v
			case bool, int, float64:
				bytes, _ := json.Marshal(v)
				valueStr = string(bytes)
			default:
				bytes, _ := json.Marshal(v)
				valueStr = string(bytes)
			}

			// Show old value if it exists
			if oldValue, exists := currentConfig[key]; exists {
				fmt.Printf("  Updating %s: %s → %s\n", key, oldValue, valueStr)
			} else {
				fmt.Printf("  Setting %s = %s\n", key, valueStr)
			}

			_, err := kvs.SetKeyValue(ctx, log, via, sd, key, valueStr)
			if err != nil {
				fmt.Printf("  ✗ Failed to set %s: %v\n", key, err)
				return nil, fmt.Errorf("failed to set KVS key %s: %w", key, err)
			}
			fmt.Printf("  ✓ Updated %s\n", key)
		}
	} else {
		fmt.Printf("\nNo KVS configuration changes specified.\n")
	}

	// Update the heater.js script
	fmt.Printf("\nUpdating heater.js script...\n")
	scriptName := "heater.js"
	buf, err := pkgscript.ReadEmbeddedFile(scriptName)
	if err != nil {
		fmt.Printf("✗ Failed to read script %s: %v\n", scriptName, err)
		return nil, err
	}

	longCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	id, err := mhscript.UploadWithVersion(longCtx, log, via, sd, scriptName, buf, !updateFlags.NoMinify, updateFlags.ForceUpload)
	if err != nil {
		fmt.Printf("✗ Failed to upload %s: %v\n", scriptName, err)
		return nil, err
	}

	if id > 0 {
		fmt.Printf("✓ Successfully uploaded %s (id: %d)\n", scriptName, id)
	} else {
		fmt.Printf("✓ Script %s is up to date (no upload needed)\n", scriptName)
	}

	fmt.Printf("\n✓ Heater update complete on %s\n", sd.Name())

	result := map[string]interface{}{
		"device":       sd.Name(),
		"script_id":    id,
		"kvs_updated":  updatesToApply,
		"kvs_previous": currentConfig,
	}

	return result, nil
}

// Helper function to parse value from KVS string
func parseKVSValue(valueStr string) interface{} {
	// Try to parse as JSON first
	var jsonValue interface{}
	if err := json.Unmarshal([]byte(valueStr), &jsonValue); err == nil {
		return jsonValue
	}

	// Try as int
	if intVal, err := strconv.Atoi(valueStr); err == nil {
		return intVal
	}

	// Try as float
	if floatVal, err := strconv.ParseFloat(valueStr, 64); err == nil {
		return floatVal
	}

	// Try as bool
	if boolVal, err := strconv.ParseBool(valueStr); err == nil {
		return boolVal
	}

	// Return as string
	return valueStr
}
