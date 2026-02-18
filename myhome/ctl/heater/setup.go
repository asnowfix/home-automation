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
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var setupFlags struct {
	// KVS configuration flags
	EnableLogging            bool
	CheapStartHour           int
	CheapEndHour             int
	PollIntervalMs           int
	PreheatHours             int
	NormallyClosed           bool
	InternalTemperatureTopic string
	ExternalTemperatureTopic string
	RoomId                   string
	DoorSensorTopics         string

	// Script upload flags
	NoMinify     bool
	ForceUpload  bool
	AutoDiscover bool
}

var setupCmd = &cobra.Command{
	Use:   "setup <device>",
	Short: "Setup heater script configuration on a Shelly device",
	Long: `Configure KVS entries for the heater script and upload/update the heater.js script.

The internal-temperature-topic, external-temperature-topic, and room-id are mandatory and must be provided.
All other configuration values have defaults.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]

		// Validate mandatory flags
		if setupFlags.InternalTemperatureTopic == "" {
			return fmt.Errorf("--internal-temperature-topic is required")
		}
		if setupFlags.ExternalTemperatureTopic == "" {
			return fmt.Errorf("--external-temperature-topic is required")
		}
		if setupFlags.RoomId == "" {
			return fmt.Errorf("--room-id is required")
		}

		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doSetup, nil)
		return err
	},
}

func init() {
	// KVS configuration flags with defaults from heater.js CONFIG_SCHEMA
	setupCmd.Flags().BoolVar(&setupFlags.NormallyClosed, string(myhome.NormallyClosedKey), true, "Whether the switch is normally closed")
	setupCmd.Flags().StringVar(&setupFlags.RoomId, string(myhome.RoomIdKey), "", "Room identifier for temperature API (required)")
	setupCmd.Flags().BoolVar(&setupFlags.EnableLogging, "enable-logging", true, "Enable logging")
	setupCmd.Flags().IntVar(&setupFlags.CheapStartHour, "cheap-start-hour", 23, "Start hour of cheap electricity window")
	setupCmd.Flags().IntVar(&setupFlags.CheapEndHour, "cheap-end-hour", 7, "End hour of cheap electricity window")
	setupCmd.Flags().IntVar(&setupFlags.PollIntervalMs, "poll-interval-ms", 300000, "Polling interval in milliseconds (default: 5 minutes)")
	setupCmd.Flags().IntVar(&setupFlags.PreheatHours, "preheat-hours", 2, "Hours before cheap window end to start preheating")
	setupCmd.Flags().StringVar(&setupFlags.InternalTemperatureTopic, "internal-temperature-topic", "", "MQTT topic for internal temperature sensor (required)")
	setupCmd.Flags().StringVar(&setupFlags.ExternalTemperatureTopic, "external-temperature-topic", "", "MQTT topic for external temperature sensor (required)")
	setupCmd.Flags().StringVar(&setupFlags.DoorSensorTopics, "door-sensor-topics", "", "Comma-separated list of MQTT topics for door/window sensors")

	// Script upload flags
	setupCmd.Flags().BoolVar(&setupFlags.NoMinify, "no-minify", false, "Do not minify script before upload")
	setupCmd.Flags().BoolVar(&setupFlags.ForceUpload, "force", false, "Force re-upload even if version hash matches")
	setupCmd.Flags().BoolVar(&setupFlags.AutoDiscover, "auto-discover", false, "Auto-discover sensors in the same room")

	// Mark mandatory flags
	setupCmd.MarkFlagRequired("internal-temperature-topic")
	setupCmd.MarkFlagRequired("external-temperature-topic")
	setupCmd.MarkFlagRequired("room-id")
}

func doSetup(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}

	fmt.Printf("Setting up heater configuration on %s...\n", sd.Name())

	// Build KVS configuration map
	kvsConfig := map[string]interface{}{
		string(myhome.NormallyClosedKey):           setupFlags.NormallyClosed,
		string(myhome.RoomIdKey):                   setupFlags.RoomId,
		"script/heater/enable-logging":             setupFlags.EnableLogging,
		"script/heater/cheap-start-hour":           setupFlags.CheapStartHour,
		"script/heater/cheap-end-hour":             setupFlags.CheapEndHour,
		"script/heater/poll-interval-ms":           setupFlags.PollIntervalMs,
		"script/heater/preheat-hours":              setupFlags.PreheatHours,
		"script/heater/internal-temperature-topic": setupFlags.InternalTemperatureTopic,
		"script/heater/external-temperature-topic": setupFlags.ExternalTemperatureTopic,
	}

	// room-id is now a device-level KVS key (unprefixed), not script-specific
	kvsConfig["room-id"] = setupFlags.RoomId

	// Also update the device's room in the database
	if setupFlags.RoomId != "" {
		params := &myhome.DeviceSetRoomParams{
			Identifier: sd.Id(),
			RoomId:     setupFlags.RoomId,
		}
		_, err := myhome.TheClient.CallE(ctx, myhome.DeviceSetRoom, params)
		if err != nil {
			fmt.Printf("  ⚠ Failed to set device room in DB: %v\n", err)
		} else {
			fmt.Printf("  ✓ Set device room in DB: %s\n", setupFlags.RoomId)
		}
	}

	// Add door sensor topics if provided or auto-discovered
	doorSensorTopics := setupFlags.DoorSensorTopics
	if setupFlags.AutoDiscover && doorSensorTopics == "" {
		// Auto-discover door sensors in the same room
		discoveredTopics, err := discoverDoorSensorsInRoom(ctx, setupFlags.RoomId)
		if err != nil {
			fmt.Printf("  ⚠ Failed to auto-discover door sensors: %v\n", err)
		} else if len(discoveredTopics) > 0 {
			doorSensorTopics = strings.Join(discoveredTopics, ",")
			fmt.Printf("  ✓ Auto-discovered door sensors: %s\n", doorSensorTopics)
		}
	}
	if doorSensorTopics != "" {
		kvsConfig["script/heater/door-sensor-topics"] = doorSensorTopics
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

// discoverDoorSensorsInRoom queries the myhome server for door/window sensors in the given room
// and returns their MQTT topics
func discoverDoorSensorsInRoom(ctx context.Context, roomId string) ([]string, error) {
	// Get devices in the room
	params := &myhome.DeviceListByRoomParams{
		RoomId: roomId,
	}
	result, err := myhome.TheClient.CallE(ctx, myhome.DeviceListByRoom, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list devices in room: %w", err)
	}

	devices := result.(*myhome.DeviceListByRoomResult).Devices
	var topics []string

	for _, d := range devices {
		// Check if device is a door/window sensor (BLU Door/Window)
		if d.Info != nil && d.Info.Model != "" {
			model := strings.ToLower(d.Info.Model)
			if strings.Contains(model, "door") || strings.Contains(model, "window") {
				// Build MQTT topic for BLU device
				if d.MAC != "" {
					topic := "shelly-blu/events/" + strings.ToLower(d.MAC)
					topics = append(topics, topic)
				}
			}
		}
	}

	return topics, nil
}
