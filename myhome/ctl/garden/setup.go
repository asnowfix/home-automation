package garden

import (
	"context"
	"fmt"
	"time"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	mhscript "github.com/asnowfix/home-automation/internal/myhome/shelly/script"
	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

const scriptName = "garden.js"
const kvsPrefix = "script/garden/"

// GardenKVSKeys (field name -> full KVS key) and ZoneFieldKeys (per-zone
// struct field -> KVS key suffix) are generated from garden.schema.json —
// see garden_defaults_generated.go and issue #439.

// defaultGlobalKVS holds the initial KVS values to write on setup.
// These match CONFIG_SCHEMA defaults in garden.js.
var defaultGlobalKVS = map[string]string{
	GardenKVSKeys["enable_logging"]:      "true",
	GardenKVSKeys["mqtt_topic_prefix"]:   "garden",
	GardenKVSKeys["earliest_start_hour"]: fmt.Sprintf("%d", DefaultEarliestStartHour),
	GardenKVSKeys["lunch_start"]:         fmt.Sprintf("%.1f", DefaultLunchStart),
	GardenKVSKeys["lunch_end"]:           fmt.Sprintf("%.1f", DefaultLunchEnd),
	GardenKVSKeys["evening_start"]:       fmt.Sprintf("%.1f", DefaultEveningStart),
	GardenKVSKeys["evening_end"]:         fmt.Sprintf("%.1f", DefaultEveningEnd),
	GardenKVSKeys["fallback_start_hour"]: fmt.Sprintf("%d", DefaultFallbackStartHour),
	GardenKVSKeys["frost_cutoff_c"]:      fmt.Sprintf("%.1f", DefaultFrostCutoffC),
	GardenKVSKeys["rain_holdoff_mm"]:     fmt.Sprintf("%.1f", DefaultRainHoldoffMm),
	GardenKVSKeys["max_deficit_mm"]:      fmt.Sprintf("%.1f", DefaultMaxDeficitMm),
}

// defaultZoneKVS generates per-zone KVS key→value pairs from ZONE_DEFAULTS
// constants. Key suffixes come from the generated ZoneFieldKeys map (mirrors
// garden.js's ZONE_KEY_SPECS) instead of hand-typed string literals.
func defaultZoneKVS() map[string]string {
	m := make(map[string]string)
	for i, z := range defaultZoneDefaults {
		pfx := kvsPrefix + fmt.Sprintf("zone%d-", i)
		m[pfx+ZoneFieldKeys["name"]] = z.name
		m[pfx+ZoneFieldKeys["appRateMmH"]] = fmt.Sprintf("%.1f", z.appRateMmH)
		m[pfx+ZoneFieldKeys["kc"]] = fmt.Sprintf("%.2f", z.kc)
		m[pfx+ZoneFieldKeys["triggerMm"]] = fmt.Sprintf("%.1f", z.triggerMm)
		m[pfx+ZoneFieldKeys["maxMin"]] = fmt.Sprintf("%d", z.maxMin)
		m[pfx+ZoneFieldKeys["fallbackMin"]] = fmt.Sprintf("%d", z.fallbackMin)
		m[pfx+ZoneFieldKeys["group"]] = z.group
		m[pfx+ZoneFieldKeys["intervalDays"]] = fmt.Sprintf("%d", z.intervalDays)
		m[pfx+ZoneFieldKeys["enabled"]] = fmt.Sprintf("%t", z.enabled)
	}
	return m
}

func getDeviceByAny(ctx context.Context, identifier string) (*myhome.Device, *shelly.Device, error) {
	devs, err := myhome.TheClient.LookupDevices(ctx, identifier)
	if err != nil || len(*devs) == 0 {
		return nil, nil, fmt.Errorf("device not found: %s", identifier)
	}
	d := (*devs)[0]
	mac := ""
	if d.Mac() != nil {
		mac = d.Mac().String()
	}
	mhDev := &myhome.Device{
		DeviceSummary: myhome.DeviceSummary{
			DeviceIdentifier: myhome.DeviceIdentifier{
				Manufacturer_: d.Manufacturer(),
				Id_:           d.Id(),
			},
			MAC:   mac,
			Host_: d.Host(),
			Name_: d.Name(),
		},
	}
	var sd *shelly.Device
	var sdErr error
	_, err = myhome.Foreach(ctx, hlog.Logger, d.Id(), types.ChannelDefault, func(ctx context.Context, log logr.Logger, via types.Channel, dev devices.Device, args []string) (any, error) {
		if s, ok := dev.(*shelly.Device); ok {
			sd = s
		} else {
			sdErr = fmt.Errorf("not a Shelly device")
		}
		return nil, nil
	}, nil)
	if err != nil {
		return nil, nil, err
	}
	if sdErr != nil {
		return nil, nil, sdErr
	}
	if sd == nil {
		return nil, nil, fmt.Errorf("Shelly device not found: %s", identifier)
	}
	return mhDev, sd, nil
}

var setupCmd = &cobra.Command{
	Use:   "setup <device>",
	Short: "Upload garden.js and configure the sprinkler device",
	Long: `Upload garden.js to the device, write initial KVS configuration, and create
the daily-plan and watering schedules.

Zone application rates and crop factors are set to conservative defaults. After
running 'ctl garden calibrate' for each zone, update the KVS key
  script/garden/zoneN-app-rate   (mm/h measured via catch-cups)
to reflect real coverage.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		identifier := args[0]

		noMinify, _ := cmd.Flags().GetBool("no-minify")
		force, _ := cmd.Flags().GetBool("force")

		_, sd, err := getDeviceByAny(ctx, identifier)
		if err != nil {
			return fmt.Errorf("device lookup failed: %w", err)
		}
		via := types.ChannelMqtt

		// 1. Write global KVS config
		fmt.Printf("Configuring global KVS for %s...\n", sd.Name())
		for key, value := range defaultGlobalKVS {
			if _, err := kvs.SetKeyValue(ctx, hlog.Logger, via, sd, key, value); err != nil {
				return fmt.Errorf("KVS write failed (%s): %w", key, err)
			}
			time.Sleep(200 * time.Millisecond)
		}

		// 2. Write per-zone KVS config
		fmt.Printf("Configuring zone KVS for %s...\n", sd.Name())
		for key, value := range defaultZoneKVS() {
			if _, err := kvs.SetKeyValue(ctx, hlog.Logger, via, sd, key, value); err != nil {
				return fmt.Errorf("KVS write failed (%s): %w", key, err)
			}
			time.Sleep(200 * time.Millisecond)
		}

		time.Sleep(500 * time.Millisecond)

		// 3. Upload and start script
		fmt.Printf("Uploading %s to %s...\n", scriptName, sd.Name())
		buf, err := pkgscript.ReadEmbeddedFile(scriptName)
		if err != nil {
			return fmt.Errorf("failed to read embedded %s: %w", scriptName, err)
		}
		minify := !noMinify
		uploadedID, err := mhscript.UploadWithVersion(ctx, hlog.Logger, via, sd, scriptName, buf, minify, force)
		if err != nil {
			return fmt.Errorf("script upload failed: %w", err)
		}
		scriptID := int(uploadedID)
		if scriptID == 0 {
			status, err := pkgscript.ScriptStatus(ctx, sd, via, scriptName)
			if err != nil || status == nil {
				return fmt.Errorf("script version unchanged but failed to get script ID: %w", err)
			}
			scriptID = int(status.Id)
			fmt.Printf("Script unchanged on %s (id:%d)\n", sd.Name(), scriptID)
		} else {
			fmt.Printf("Script uploaded and started on %s (id:%d)\n", sd.Name(), scriptID)
		}

		time.Sleep(2 * time.Second)

		// 4. Create schedules via script.eval
		fmt.Printf("Creating schedules via script.eval...\n")
		code := fmt.Sprintf("clearNonUpdateSchedules(function(){createSchedules(null)})")
		if _, err := pkgscript.EvalInDevice(ctx, via, sd, scriptName, code); err != nil {
			fmt.Printf("  Warning: schedule creation may have failed: %v\n", err)
		} else {
			fmt.Printf("Schedules created\n")
		}

		fmt.Printf("\nSetup complete for %s (%s)\n", sd.Name(), sd.Id())
		fmt.Printf("Next steps:\n")
		fmt.Printf("  1. Run 'ctl garden calibrate %s <zone> <minutes>' for each zone\n", identifier)
		fmt.Printf("  2. Measure applied depth with catch-cups\n")
		fmt.Printf("  3. Update zone app-rate: KVS key script/garden/zoneN-app-rate (mm/h)\n")
		return nil
	},
}

func init() {
	gardenCmd.AddCommand(setupCmd)
	setupCmd.Flags().Bool("no-minify", false, "Do not minify script before upload")
	setupCmd.Flags().Bool("force", false, "Force re-upload even if version hash matches")
}
