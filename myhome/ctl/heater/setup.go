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
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var setupFlags struct {
	// KVS configuration flags
	EnableLogging            bool
	SetPoint                 float64
	MinInternalTemp          float64
	CheapStartHour           int
	CheapEndHour             int
	PollIntervalMs           int
	PreheatHours             int
	NormallyClosed           bool
	InternalTemperatureTopic string
	ExternalTemperatureTopic string
	RoomId                   string

	// Script upload flags
	NoMinify    bool
	ForceUpload bool
}

var setupCmd = &cobra.Command{
	Use:   "setup <device>",
	Short: "Setup heater script configuration on a Shelly device",
	Long: `Configure KVS entries for the heater script and upload/update the heater.js script.

The internal-temperature-topic and external-temperature-topic are mandatory and must be provided.
All other configuration values have defaults.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]

		// Validate mandatory topics
		if setupFlags.InternalTemperatureTopic == "" {
			return fmt.Errorf("--internal-temperature-topic is required")
		}
		if setupFlags.ExternalTemperatureTopic == "" {
			return fmt.Errorf("--external-temperature-topic is required")
		}

		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doSetup, nil)
		return err
	},
}

func init() {
	// KVS configuration flags with defaults from heater.js CONFIG_SCHEMA
	setupCmd.Flags().BoolVar(&setupFlags.EnableLogging, "enable-logging", true, "Enable logging")
	setupCmd.Flags().Float64Var(&setupFlags.SetPoint, "set-point", 19.0, "Target temperature")
	setupCmd.Flags().Float64Var(&setupFlags.MinInternalTemp, "min-internal-temp", 15.0, "Minimum internal temperature threshold")
	setupCmd.Flags().IntVar(&setupFlags.CheapStartHour, "cheap-start-hour", 23, "Start hour of cheap electricity window")
	setupCmd.Flags().IntVar(&setupFlags.CheapEndHour, "cheap-end-hour", 7, "End hour of cheap electricity window")
	setupCmd.Flags().IntVar(&setupFlags.PollIntervalMs, "poll-interval-ms", 300000, "Polling interval in milliseconds (default: 5 minutes)")
	setupCmd.Flags().IntVar(&setupFlags.PreheatHours, "preheat-hours", 2, "Hours before cheap window end to start preheating")
	setupCmd.Flags().BoolVar(&setupFlags.NormallyClosed, "normally-closed", true, "Whether the switch is normally closed")
	setupCmd.Flags().StringVar(&setupFlags.InternalTemperatureTopic, "internal-temperature-topic", "", "MQTT topic for internal temperature sensor (required)")
	setupCmd.Flags().StringVar(&setupFlags.ExternalTemperatureTopic, "external-temperature-topic", "", "MQTT topic for external temperature sensor (required)")
	setupCmd.Flags().StringVar(&setupFlags.RoomId, "room-id", "", "Room identifier for temperature API")

	// Script upload flags
	setupCmd.Flags().BoolVar(&setupFlags.NoMinify, "no-minify", false, "Do not minify script before upload")
	setupCmd.Flags().BoolVar(&setupFlags.ForceUpload, "force", false, "Force re-upload even if version hash matches")

	// Mark mandatory flags
	setupCmd.MarkFlagRequired("internal-temperature-topic")
	setupCmd.MarkFlagRequired("external-temperature-topic")
}

func doSetup(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	fmt.Printf("Setting up heater configuration on %s...\n", sd.Name())

	// Build KVS configuration map
	kvsConfig := map[string]interface{}{
		"script/heater/enable-logging":             setupFlags.EnableLogging,
		"script/heater/set-point":                  setupFlags.SetPoint,
		"script/heater/min-internal-temp":          setupFlags.MinInternalTemp,
		"script/heater/cheap-start-hour":           setupFlags.CheapStartHour,
		"script/heater/cheap-end-hour":             setupFlags.CheapEndHour,
		"script/heater/poll-interval-ms":           setupFlags.PollIntervalMs,
		"script/heater/preheat-hours":              setupFlags.PreheatHours,
		"normally-closed":                          setupFlags.NormallyClosed,
		"script/heater/internal-temperature-topic": setupFlags.InternalTemperatureTopic,
		"script/heater/external-temperature-topic": setupFlags.ExternalTemperatureTopic,
	}

	// Add room-id only if provided
	if setupFlags.RoomId != "" {
		kvsConfig["script/heater/room-id"] = setupFlags.RoomId
	}

	// Set each KVS entry
	fmt.Printf("\nConfiguring KVS entries:\n")
	for key, value := range kvsConfig {
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

		fmt.Printf("  Setting %s = %s\n", key, valueStr)
		_, err := kvs.SetKeyValue(ctx, log, via, sd, key, valueStr)
		if err != nil {
			fmt.Printf("  ✗ Failed to set %s: %v\n", key, err)
			return nil, fmt.Errorf("failed to set KVS key %s: %w", key, err)
		}
		fmt.Printf("  ✓ Set %s\n", key)
	}

	// Upload the heater.js script
	fmt.Printf("\nUploading heater.js script...\n")
	scriptName := "heater.js"
	buf, err := pkgscript.ReadEmbeddedFile(scriptName)
	if err != nil {
		fmt.Printf("✗ Failed to read script %s: %v\n", scriptName, err)
		return nil, err
	}

	longCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	id, err := mhscript.UploadWithVersion(longCtx, log, via, sd, scriptName, buf, !setupFlags.NoMinify, setupFlags.ForceUpload)
	if err != nil {
		fmt.Printf("✗ Failed to upload %s: %v\n", scriptName, err)
		return nil, err
	}

	fmt.Printf("✓ Successfully uploaded %s (id: %d)\n", scriptName, id)
	fmt.Printf("\n✓ Heater setup complete on %s\n", sd.Name())

	return map[string]interface{}{
		"device":     sd.Name(),
		"script_id":  id,
		"kvs_config": kvsConfig,
	}, nil
}
