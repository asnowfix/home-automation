package db

import (
	"encoding/json"
	"fmt"
	"io"
	"myhome"
	"os"

	"github.com/spf13/cobra"
)

// DatabaseImport represents the complete database import format
// Supports both unified format and legacy devices-only format
type DatabaseImport struct {
	Devices          []myhome.Device                          `json:"devices,omitempty"`
	TemperatureRooms map[string]*myhome.TemperatureRoomConfig `json:"temperature_rooms,omitempty"`
	WeekdayDefaults  map[int]myhome.DayType                   `json:"weekday_defaults,omitempty"`
	KindSchedules    []myhome.TemperatureKindSchedule         `json:"kind_schedules,omitempty"`
}

var importFlags struct {
	Overwrite   bool
	DevicesOnly bool
}

var ImportCmd = &cobra.Command{
	Use:   "import [file]",
	Short: "Import database from JSON",
	Long: `Import database from a JSON file.

Supports both the unified export format (devices + temperature tables) and legacy devices-only format.
The input can be read from a file or from stdin.

To import to a specific myhome server instance, use the -I flag:
  myhome ctl -I myhome-local db import database.json

Examples:
  # Import entire database from a file
  myhome ctl db import database.json

  # Import from stdin
  cat database.json | myhome ctl db import

  # Import only devices (skip temperature tables)
  myhome ctl db import database.json --devices-only

  # Import to a specific myhome instance (e.g., local dev server)
  myhome ctl -I myhome-local db import database.json

  # Export from default instance and import to local instance
  myhome ctl db export --output database.json
  myhome ctl -I myhome-local db import database.json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		var data []byte
		var err error

		if len(args) == 1 {
			// Read from file
			data, err = os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
		} else {
			// Read from stdin
			data, err = io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read from stdin: %w", err)
			}
		}

		if len(data) == 0 {
			return fmt.Errorf("no data to import")
		}

		// Try to parse as unified format first
		var dbImport DatabaseImport
		err = json.Unmarshal(data, &dbImport)
		if err != nil {
			// Try legacy devices-only format (array of devices)
			var devices []myhome.Device
			err = json.Unmarshal(data, &devices)
			if err != nil {
				return fmt.Errorf("failed to parse JSON: %w", err)
			}
			dbImport.Devices = devices
		}

		// Track import counts
		deviceCount := 0
		roomCount := 0
		weekdayCount := 0
		scheduleCount := 0

		// Import devices
		if len(dbImport.Devices) > 0 {
			for _, device := range dbImport.Devices {
				// Check if context was cancelled (e.g., Ctrl-C)
				if ctx.Err() != nil {
					return ctx.Err()
				}
				_, err := myhome.TheClient.CallE(ctx, myhome.DeviceUpdate, &device)
				if err != nil {
					// Check if this is a context cancellation error
					if ctx.Err() != nil {
						return ctx.Err()
					}
					fmt.Fprintf(os.Stderr, "⚠ Failed to import device %s: %v\n", device.Id(), err)
					continue
				}
				deviceCount++
				fmt.Fprintf(os.Stderr, "✓ Imported device %s (%s)\n", device.Name(), device.Id())
			}
		}

		// Import temperature tables unless --devices-only is set
		if !importFlags.DevicesOnly {
			// Import temperature rooms
			for roomID, roomConfig := range dbImport.TemperatureRooms {
				params := &myhome.TemperatureSetParams{
					RoomID: roomID,
					Name:   roomConfig.Name,
					Kinds:  roomConfig.Kinds,
					Levels: roomConfig.Levels,
				}
				_, err := myhome.TheClient.CallE(ctx, myhome.TemperatureSet, params)
				if err != nil {
					fmt.Fprintf(os.Stderr, "⚠ Failed to import room %s: %v\n", roomID, err)
					continue
				}
				roomCount++
				fmt.Fprintf(os.Stderr, "✓ Imported room %s (%s)\n", roomConfig.Name, roomID)
			}

			// Import weekday defaults
			for weekday, dayType := range dbImport.WeekdayDefaults {
				params := &myhome.TemperatureSetWeekdayDefaultParams{
					Weekday: weekday,
					DayType: dayType,
				}
				_, err := myhome.TheClient.CallE(ctx, myhome.TemperatureSetWeekdayDefault, params)
				if err != nil {
					fmt.Fprintf(os.Stderr, "⚠ Failed to import weekday default %d: %v\n", weekday, err)
					continue
				}
				weekdayCount++
			}
			if weekdayCount > 0 {
				fmt.Fprintf(os.Stderr, "✓ Imported %d weekday defaults\n", weekdayCount)
			}

			// Import kind schedules
			for _, schedule := range dbImport.KindSchedules {
				// Convert TemperatureTimeRange to string format
				rangeStrs := make([]string, len(schedule.Ranges))
				for i, r := range schedule.Ranges {
					rangeStrs[i] = fmt.Sprintf("%02d:%02d-%02d:%02d",
						r.Start/60, r.Start%60, r.End/60, r.End%60)
				}

				params := &myhome.TemperatureSetKindScheduleParams{
					Kind:    schedule.Kind,
					DayType: schedule.DayType,
					Ranges:  rangeStrs,
				}
				_, err := myhome.TheClient.CallE(ctx, myhome.TemperatureSetKindSchedule, params)
				if err != nil {
					fmt.Fprintf(os.Stderr, "⚠ Failed to import kind schedule %s/%s: %v\n", schedule.Kind, schedule.DayType, err)
					continue
				}
				scheduleCount++
			}
			if scheduleCount > 0 {
				fmt.Fprintf(os.Stderr, "✓ Imported %d kind schedules\n", scheduleCount)
			}
		}

		// Print summary
		fmt.Fprintf(os.Stderr, "\n✓ Import complete:\n")
		fmt.Fprintf(os.Stderr, "  - %d devices\n", deviceCount)
		if !importFlags.DevicesOnly {
			fmt.Fprintf(os.Stderr, "  - %d temperature rooms\n", roomCount)
			fmt.Fprintf(os.Stderr, "  - %d weekday defaults\n", weekdayCount)
			fmt.Fprintf(os.Stderr, "  - %d kind schedules\n", scheduleCount)
		}

		return nil
	},
}

func init() {
	ImportCmd.Flags().BoolVar(&importFlags.Overwrite, "overwrite", true, "Overwrite existing entries (default: true)")
	ImportCmd.Flags().BoolVar(&importFlags.DevicesOnly, "devices-only", false, "Import only devices (skip temperature tables)")
}
