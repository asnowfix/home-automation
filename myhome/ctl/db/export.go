package db

import (
	"encoding/json"
	"fmt"
	"github.com/asnowfix/home-automation/internal/myhome"
	"os"

	"github.com/spf13/cobra"
)

// DatabaseExport is the dump file format, shared with import.
type DatabaseExport = myhome.DatabaseDump

var exportFlags struct {
	Output      string
	Pretty      bool
	DevicesOnly bool
}

var ExportCmd = &cobra.Command{
	Use:   "export [pattern]",
	Short: "Export database to JSON",
	Long: `Export the complete database to JSON format.

By default, exports all tables: devices, temperature rooms, weekday defaults, and kind schedules.
Use --devices-only to export only devices.

The output can be written to stdout (default) or to a file using --output.
Use a pattern to filter which devices to export (e.g., "shellyblu*").

To export from a specific myhome server instance, use the -I flag:
  myhome ctl -I myhome-local db export

Examples:
  # Export entire database to stdout
  myhome ctl db export

  # Export entire database to a file
  myhome ctl db export --output database.json

  # Export only devices matching a pattern
  myhome ctl db export "shellyblu*" --devices-only

  # Export with pretty formatting
  myhome ctl db export --pretty

  # Export from a specific myhome instance
  myhome ctl -I myhome-local db export --output local-database.json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		pattern := "*"
		if len(args) == 1 {
			pattern = args[0]
		}

		export := DatabaseExport{}
		deviceCount := 0

		// Full device records (room assignment, info, config) read from the
		// database by the daemon, not from its in-memory view (#775).
		out, err := myhome.TheClient.CallE(ctx, myhome.DeviceExport, pattern)
		if err != nil {
			return fmt.Errorf("failed to export devices: %w", err)
		}
		devices, ok := out.(*[]*myhome.Device)
		if !ok {
			return fmt.Errorf("unexpected reply %T from %s", out, myhome.DeviceExport)
		}
		if len(*devices) > 0 {
			export.Devices = *devices
			deviceCount = len(*devices)
		}

		// Export temperature tables unless --devices-only is set
		if !exportFlags.DevicesOnly {
			// Get temperature rooms
			roomsResult, err := myhome.TheClient.CallE(ctx, myhome.TemperatureList, nil)
			if err == nil {
				if rooms, ok := roomsResult.(*myhome.TemperatureRoomList); ok && len(*rooms) > 0 {
					export.TemperatureRooms = *rooms
				}
			}

			// Get weekday defaults
			weekdayResult, err := myhome.TheClient.CallE(ctx, myhome.TemperatureGetWeekdayDefaults, &myhome.TemperatureGetWeekdayDefaultsParams{})
			if err == nil {
				if weekdayDefaults, ok := weekdayResult.(*myhome.TemperatureWeekdayDefaults); ok && len(weekdayDefaults.Defaults) > 0 {
					export.WeekdayDefaults = weekdayDefaults.Defaults
				}
			}

			// Get kind schedules
			kindSchedulesResult, err := myhome.TheClient.CallE(ctx, myhome.TemperatureGetKindSchedules, &myhome.TemperatureGetKindSchedulesParams{})
			if err == nil {
				if kindSchedules, ok := kindSchedulesResult.(*myhome.TemperatureKindScheduleList); ok && len(*kindSchedules) > 0 {
					export.KindSchedules = *kindSchedules
				}
			}
		}

		// Check if we have anything to export
		roomCount := len(export.TemperatureRooms)
		weekdayCount := len(export.WeekdayDefaults)
		scheduleCount := len(export.KindSchedules)

		if deviceCount == 0 && roomCount == 0 && weekdayCount == 0 && scheduleCount == 0 {
			fmt.Fprintln(os.Stderr, "No data found to export")
			return nil
		}

		// Marshal to JSON
		var data []byte
		if exportFlags.Pretty {
			data, err = json.MarshalIndent(export, "", "  ")
		} else {
			data, err = json.Marshal(export)
		}
		if err != nil {
			return fmt.Errorf("failed to marshal database: %w", err)
		}

		// Write output
		if exportFlags.Output != "" {
			err = os.WriteFile(exportFlags.Output, data, 0644)
			if err != nil {
				return fmt.Errorf("failed to write to file: %w", err)
			}
			fmt.Fprintf(os.Stderr, "✓ Exported to %s:\n", exportFlags.Output)
		} else {
			fmt.Println(string(data))
		}

		// Print summary to stderr
		fmt.Fprintf(os.Stderr, "  - %d devices\n", deviceCount)
		if !exportFlags.DevicesOnly {
			fmt.Fprintf(os.Stderr, "  - %d temperature rooms\n", roomCount)
			fmt.Fprintf(os.Stderr, "  - %d weekday defaults\n", weekdayCount)
			fmt.Fprintf(os.Stderr, "  - %d kind schedules\n", scheduleCount)
		}

		return nil
	},
}

func init() {
	ExportCmd.Flags().StringVarP(&exportFlags.Output, "output", "o", "", "Output file (default: stdout)")
	ExportCmd.Flags().BoolVar(&exportFlags.Pretty, "pretty", false, "Pretty-print JSON output")
	ExportCmd.Flags().BoolVar(&exportFlags.DevicesOnly, "devices-only", false, "Export only devices (skip temperature tables)")
}
