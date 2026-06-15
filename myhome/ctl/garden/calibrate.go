package garden

import (
	"fmt"
	"strconv"

	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/spf13/cobra"
)

var calibrateCmd = &cobra.Command{
	Use:   "calibrate <device> <zone> <minutes>",
	Short: "Run one zone for calibration and measure mm/min with catch-cups",
	Long: `Run a single watering zone for exactly <minutes> minutes without touching
the water-balance deficit model. Use catch-cups or a rain gauge to measure the
applied depth, then update the zone's application rate:

  KVS key:  script/garden/zoneN-app-rate   (value in mm/h)

Zone IDs: 0=pelouse-maison  1=massifs  2=pelouse-barriere

The calibration is triggered via script.eval and runs on-device. Quiet windows
(12:00-14:00, 19:00-23:30) are honoured; the request will be rejected if called
during those windows.`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		zoneID, err := strconv.Atoi(args[1])
		if err != nil || zoneID < 0 || zoneID > 2 {
			return fmt.Errorf("zone must be 0, 1, or 2 (got %q)", args[1])
		}
		minutes, err := strconv.Atoi(args[2])
		if err != nil || minutes <= 0 || minutes > 60 {
			return fmt.Errorf("minutes must be between 1 and 60 (got %q)", args[2])
		}

		_, sd, err := getDeviceByAny(ctx, args[0])
		if err != nil {
			return fmt.Errorf("device lookup failed: %w", err)
		}
		via := types.ChannelMqtt

		code := fmt.Sprintf("handleCalibrate(%d, %d)", zoneID, minutes)
		if _, err := pkgscript.EvalInDevice(ctx, via, sd, scriptName, code); err != nil {
			return fmt.Errorf("calibrate eval failed: %w", err)
		}

		fmt.Printf("Calibration started: zone %d (%s) for %d minutes on %s\n",
			zoneID, defaultZoneDefaults[zoneID].name, minutes, sd.Name())
		fmt.Printf("After the run, measure applied depth with catch-cups and set:\n")
		fmt.Printf("  KVS key: script/garden/zone%d-app-rate = <measured mm/h>\n", zoneID)
		return nil
	},
}

func init() {
	gardenCmd.AddCommand(calibrateCmd)
}
