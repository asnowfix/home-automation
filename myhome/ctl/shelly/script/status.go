package script

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"pkg/devices"
	"pkg/shelly"
	"pkg/shelly/kvs"
	"pkg/shelly/script"
	"pkg/shelly/types"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

// StatusWithVersion extends script.Status with version tracking information for CLI display
type StatusWithVersion struct {
	script.Status                                     // Embed the native Shelly status
	Version       string `json:"version,omitempty"`  // SHA1 hash of the script version on the device (from KVS)
	UpToDate      *bool  `json:"up_to_date,omitempty"` // Whether the script is up-to-date with the embedded version (nil if unknown)
}

func init() {
	Cmd.AddCommand(statusCtl)
}

var statusCtl = &cobra.Command{
	Use:   "status",
	Short: "Report status of all scripts loaded on the given Shelly device(s)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, args[0], options.Via, doStatus, options.Args(args))
		return err
	},
}

func doStatus(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd, ok := device.(*shelly.Device)
	if !ok {
		return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
	}
	
	var err error
	if len(args) > 0 {
		// Single script status - return as-is
		out, err := script.ScriptStatus(ctx, sd, via, args[0])
		if err != nil {
			hlog.Logger.Error(err, "Unable to get script status")
			return nil, err
		}
		options.PrintResult(out)
		return out, nil
	}
	
	// All scripts status - enhance with version information
	statuses, err := script.DeviceStatus(ctx, sd, via)
	if err != nil {
		hlog.Logger.Error(err, "Unable to get scripts status")
		return nil, err
	}
	
	// Enhance each status with version information
	enhancedStatuses := make([]StatusWithVersion, 0, len(statuses))
	for _, s := range statuses {
		enhanced := StatusWithVersion{
			Status: s,
		}
		
		// Only check version for non-manual scripts (embedded scripts)
		if !s.Manual {
			// Get the version from KVS
			kvsKey := fmt.Sprintf("script/%s", s.Name)
			res, err := kvs.GetValue(ctx, log, via, sd, kvsKey)
			if err == nil && res != nil {
				enhanced.Version = res.Value
				
				// Compute the expected version from embedded script
				buf, err := script.ReadEmbeddedFile(s.Name)
				if err == nil {
					h := sha1.New()
					h.Write(buf)
					expectedVersion := hex.EncodeToString(h.Sum(nil))
					
					// Compare versions
					upToDate := (enhanced.Version == expectedVersion)
					enhanced.UpToDate = &upToDate
				}
			}
		}
		
		enhancedStatuses = append(enhancedStatuses, enhanced)
	}
	
	options.PrintResult(enhancedStatuses)
	return enhancedStatuses, nil
}
