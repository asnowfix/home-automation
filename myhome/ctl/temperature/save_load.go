package temperature

import (
	"encoding/json"
	"fmt"
	"io"
	"myhome"
	"myhome/ctl/options"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// TemperatureConfig represents the complete temperature configuration
type TemperatureConfig struct {
	Rooms           map[string]*myhome.TemperatureRoomConfig `json:"rooms" yaml:"rooms"`
	WeekdayDefaults map[int]myhome.DayType                   `json:"weekday_defaults" yaml:"weekday_defaults"`
	KindSchedules   []myhome.TemperatureKindSchedule         `json:"kind_schedules" yaml:"kind_schedules"`
}

var saveCmd = &cobra.Command{
	Use:   "save",
	Short: "Save entire temperature configuration to stdout",
	Long: `Save the complete temperature configuration (rooms, weekday defaults, kind schedules) to stdout in JSON or YAML format.

Use --json flag for JSON output, otherwise YAML is used.

Examples:
  # Save as YAML
  myhome ctl temperature save > temperature-config.yaml
  
  # Save as JSON
  myhome ctl temperature save --json > temperature-config.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Get all rooms
		roomsResult, err := myhome.TheClient.CallE(ctx, myhome.TemperatureList, nil)
		if err != nil {
			return fmt.Errorf("failed to get rooms: %w", err)
		}
		rooms, ok := roomsResult.(*myhome.TemperatureRoomList)
		if !ok {
			return fmt.Errorf("unexpected result type for rooms")
		}

		// Get weekday defaults
		weekdayResult, err := myhome.TheClient.CallE(ctx, myhome.TemperatureGetWeekdayDefaults, &myhome.TemperatureGetWeekdayDefaultsParams{})
		if err != nil {
			return fmt.Errorf("failed to get weekday defaults: %w", err)
		}
		weekdayDefaults, ok := weekdayResult.(*myhome.TemperatureWeekdayDefaults)
		if !ok {
			return fmt.Errorf("unexpected result type for weekday defaults")
		}

		// Get kind schedules
		kindSchedulesResult, err := myhome.TheClient.CallE(ctx, myhome.TemperatureGetKindSchedules, &myhome.TemperatureGetKindSchedulesParams{})
		if err != nil {
			return fmt.Errorf("failed to get kind schedules: %w", err)
		}
		kindSchedules, ok := kindSchedulesResult.(*myhome.TemperatureKindScheduleList)
		if !ok {
			return fmt.Errorf("unexpected result type for kind schedules")
		}

		// Build complete config
		config := TemperatureConfig{
			Rooms:           *rooms,
			WeekdayDefaults: weekdayDefaults.Defaults,
			KindSchedules:   *kindSchedules,
		}

		// Output as JSON or YAML
		return options.PrintResult(config)
	},
}

var loadCmd = &cobra.Command{
	Use:   "load",
	Short: "Load entire temperature configuration from stdin",
	Long: `Load the complete temperature configuration (rooms, weekday defaults, kind schedules) from stdin in JSON or YAML format.

The format is auto-detected. Use the same format that was produced by 'save' command.

Examples:
  # Load from YAML file
  myhome ctl temperature load < temperature-config.yaml
  
  # Load from JSON file
  myhome ctl temperature load < temperature-config.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Read from stdin
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %w", err)
		}

		// Try to parse as JSON first
		var config TemperatureConfig
		err = json.Unmarshal(data, &config)
		if err != nil {
			// Try YAML
			err = yaml.Unmarshal(data, &config)
			if err != nil {
				return fmt.Errorf("failed to parse input as JSON or YAML: %w", err)
			}
		}

		// Load rooms
		roomCount := 0
		for roomID, roomConfig := range config.Rooms {
			params := &myhome.TemperatureSetParams{
				RoomID: roomID,
				Name:   roomConfig.Name,
				Kinds:  roomConfig.Kinds,
				Levels: roomConfig.Levels,
			}
			_, err := myhome.TheClient.CallE(ctx, myhome.TemperatureSet, params)
			if err != nil {
				return fmt.Errorf("failed to set room %s: %w", roomID, err)
			}
			roomCount++
		}

		// Load weekday defaults
		weekdayCount := 0
		for weekday, dayType := range config.WeekdayDefaults {
			params := &myhome.TemperatureSetWeekdayDefaultParams{
				Weekday: weekday,
				DayType: dayType,
			}
			_, err := myhome.TheClient.CallE(ctx, myhome.TemperatureSetWeekdayDefault, params)
			if err != nil {
				return fmt.Errorf("failed to set weekday default for weekday %d: %w", weekday, err)
			}
			weekdayCount++
		}

		// Load kind schedules
		kindScheduleCount := 0
		for _, schedule := range config.KindSchedules {
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
				return fmt.Errorf("failed to set kind schedule for %s/%s: %w", schedule.Kind, schedule.DayType, err)
			}
			kindScheduleCount++
		}

		fmt.Printf("✓ Loaded temperature configuration:\n")
		fmt.Printf("  - %d rooms\n", roomCount)
		fmt.Printf("  - %d weekday defaults\n", weekdayCount)
		fmt.Printf("  - %d kind schedules\n", kindScheduleCount)

		return nil
	},
}
