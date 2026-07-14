package blu

import (
	"context"
	"fmt"
	"strings"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/internal/tools"
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

	mac, err = macFromBluDeviceID(deviceID)
	if err != nil {
		return "", fmt.Errorf("device %q: %w", identifier, err)
	}
	return mac, nil
}

// macFromBluDeviceID extracts and normalizes the MAC address from a Shelly BLU device ID.
// BLU device IDs follow the pattern <model>-<mac12hex> where model starts with "shellyblu".
// Examples: "shellyblu-e8e07ea60c6f", "shellyblumotion1-e8e07ed0f989", "shellyblubutton1-..."
func macFromBluDeviceID(deviceID string) (string, error) {
	if !strings.HasPrefix(deviceID, "shellyblu") {
		return "", fmt.Errorf("not a BLU device (id: %s)", deviceID)
	}
	parts := strings.Split(deviceID, "-")
	mac := tools.NormalizeMac(parts[len(parts)-1])
	if !isValidMac(mac) {
		return "", fmt.Errorf("failed to extract valid MAC from BLU device ID %q", deviceID)
	}
	return mac, nil
}

// isValidMac checks if a string is a valid colon-separated MAC address
func isValidMac(mac string) bool {
	// Valid MAC: "e8:e0:7e:a6:0c:6f" (17 chars, 5 colons)
	return len(mac) == 17 && strings.Count(mac, ":") == 5
}
