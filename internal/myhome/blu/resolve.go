package blu

import (
	"context"
	"fmt"
	"myhome"
	"strings"
	"tools"
)

// ResolveMac resolves a BLU device identifier (MAC address, device ID, or device name)
// to a normalized MAC address. It first tries to normalize as a MAC address, then falls
// back to looking up the device in the registry.
//
// Supported identifier formats:
//   - MAC address: "e8:e0:7e:a6:0c:6f", "E8E07EA60C6F", "e8-e0-7e-a6-0c-6f"
//   - Device ID: "shellyblu-e8e07ea60c6f"
//   - Device name: "motion-sensor-hallway"
func ResolveMac(ctx context.Context, identifier string) (string, error) {
	// First, try to normalize as a MAC address directly
	mac := tools.NormalizeMac(identifier)
	if mac != "" && isValidMac(mac) {
		return mac, nil
	}

	// If not a valid MAC, try to look up the device by identifier
	devices, err := myhome.TheClient.LookupDevices(ctx, identifier)
	if err != nil {
		return "", fmt.Errorf("failed to lookup BLU device %q: %w", identifier, err)
	}

	if devices == nil || len(*devices) == 0 {
		return "", fmt.Errorf("no BLU device found for identifier %q", identifier)
	}

	if len(*devices) > 1 {
		return "", fmt.Errorf("multiple devices match identifier %q, please be more specific", identifier)
	}

	device := (*devices)[0]
	deviceID := device.Id()

	// Check if this is a BLU device (ID starts with "shellyblu-")
	if !strings.HasPrefix(deviceID, "shellyblu-") {
		return "", fmt.Errorf("device %q is not a BLU device (id: %s)", identifier, deviceID)
	}

	// Extract MAC from device ID: "shellyblu-e8e07ea60c6f" -> "e8e07ea60c6f"
	macHex := strings.TrimPrefix(deviceID, "shellyblu-")

	// Normalize to colon-separated format
	mac = tools.NormalizeMac(macHex)
	if mac == "" || !isValidMac(mac) {
		return "", fmt.Errorf("failed to extract valid MAC from device %q (id: %s)", identifier, deviceID)
	}

	return mac, nil
}

// isValidMac checks if a string is a valid colon-separated MAC address
func isValidMac(mac string) bool {
	// Valid MAC: "e8:e0:7e:a6:0c:6f" (17 chars, 5 colons)
	return len(mac) == 17 && strings.Count(mac, ":") == 5
}
