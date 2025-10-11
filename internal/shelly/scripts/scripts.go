package scripts

import (
	"context"
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"pkg/shelly/kvs"
	"pkg/shelly/script"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

//go:embed *.js
var content embed.FS

// GetFS returns the embedded filesystem containing all Shelly scripts
func GetFS() fs.FS {
	return content
}

// StatusWithVersion extends script.Status with version tracking information
type StatusWithVersion struct {
	script.Status        // Embed the native Shelly status
	Version       string `json:"version,omitempty"`    // SHA1 hash of the script version on the device (from KVS)
	UpToDate      *bool  `json:"up_to_date,omitempty"` // Whether the script is up-to-date with the embedded version (nil if unknown)
}

// ComputeScriptVersion computes the SHA1 hash of a script file
func ComputeScriptVersion(name string) (string, error) {
	buf, err := fs.ReadFile(content, name)
	if err != nil {
		return "", err
	}
	h := sha1.New()
	h.Write(buf)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// DeviceStatusWithVersion returns enhanced status information including version tracking
func DeviceStatusWithVersion(ctx context.Context, log logr.Logger, device types.Device, via types.Channel) ([]StatusWithVersion, error) {
	statuses, err := script.DeviceStatus(ctx, device, via)
	if err != nil {
		return nil, err
	}

	enhancedStatuses := make([]StatusWithVersion, 0, len(statuses))
	for _, s := range statuses {
		enhanced := StatusWithVersion{
			Status: s,
		}

		// Only check version for non-manual scripts (embedded scripts)
		if !s.Manual {
			// Get the version from KVS
			kvsKey := fmt.Sprintf("script/%s", s.Name)
			res, err := kvs.GetValue(ctx, log, via, device, kvsKey)
			if err == nil && res != nil {
				enhanced.Version = res.Value

				// Compute the expected version from embedded script
				expectedVersion, err := ComputeScriptVersion(s.Name)
				if err == nil {
					// Compare versions
					upToDate := (enhanced.Version == expectedVersion)
					enhanced.UpToDate = &upToDate
				}
			}
		}

		enhancedStatuses = append(enhancedStatuses, enhanced)
	}

	return enhancedStatuses, nil
}
