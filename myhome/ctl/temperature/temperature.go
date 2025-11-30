package temperature

import (
	"fmt"
	"myhome"
	"myhome/ctl/options"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "temperature",
	Short: "Manage temperature configurations",
	Long: `Manage room temperature configurations including temperature levels, room kinds, weekday defaults, and room kind schedules.

Room kinds: bedroom, office, living-room, kitchen, other
Temperature levels: eco (default), comfort, away`,
}

func init() {
	Cmd.AddCommand(getCmd)
	Cmd.AddCommand(setCmd)
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(deleteCmd)
	Cmd.AddCommand(scheduleCmd)
	Cmd.AddCommand(weekdayCmd)
	Cmd.AddCommand(kindScheduleCmd)
	Cmd.AddCommand(saveCmd)
	Cmd.AddCommand(loadCmd)
}

// ============================================================================
// Room Configuration Commands
// ============================================================================

var getCmd = &cobra.Command{
	Use:   "get <room-id>",
	Short: "Get temperature configuration for a room",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		roomID := args[0]

		params := &myhome.TemperatureGetParams{
			RoomID: roomID,
		}

		result, err := myhome.TheClient.CallE(ctx, myhome.TemperatureGet, params)
		if err != nil {
			return err
		}

		config, ok := result.(*myhome.TemperatureRoomConfig)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		// Use JSON/YAML output if flag is set
		if options.Flags.Json || cmd.Flags().Changed("output") {
			return options.PrintResult(config)
		}

		// Display result in human-readable format
		fmt.Printf("Room: %s (%s)\n", config.Name, config.RoomID)
		fmt.Printf("Room Kinds: %s\n", formatKinds(config.Kinds))
		fmt.Printf("\nTemperature Levels:\n")
		for level, temp := range config.Levels {
			fmt.Printf("  %s: %.1f°C\n", level, temp)
		}

		return nil
	},
}

var setCmd = &cobra.Command{
	Use:   "set <room-id-pattern>",
	Short: "Set temperature configuration for room(s)",
	Long: `Set or update temperature configuration for one or more rooms.

Supports wildcards: '*' matches all rooms, 'chambre*' matches rooms starting with 'chambre', '*bureau' matches rooms ending with 'bureau'.

Only specified flags are updated - unspecified values remain unchanged for existing rooms.
For new rooms, --name, --kinds, and --eco are required.
	
Room kinds: bedroom, office, living-room, kitchen, other
Temperature levels: eco (required for new rooms), comfort, away

Examples:
  # Create new room with full configuration
  myhome ctl temperature set living-room --name "Living Room" --kinds living-room --eco 17 --comfort 21

  # Update only away temperature for all rooms
  myhome ctl temperature set '*' --away 14

  # Update comfort temperature for all rooms starting with 'chambre'
  myhome ctl temperature set 'chambre*' --comfort 19

  # Update eco temperature for specific room
  myhome ctl temperature set salon --eco 16`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		pattern := args[0]

		// Get flag values
		name, _ := cmd.Flags().GetString("name")
		kindsStr, _ := cmd.Flags().GetString("kinds")
		eco, _ := cmd.Flags().GetFloat64("eco")
		comfort, _ := cmd.Flags().GetFloat64("comfort")
		away, _ := cmd.Flags().GetFloat64("away")

		// Check which flags were explicitly set
		nameSet := cmd.Flags().Changed("name")
		kindsSet := cmd.Flags().Changed("kinds")
		ecoSet := cmd.Flags().Changed("eco")
		comfortSet := cmd.Flags().Changed("comfort")
		awaySet := cmd.Flags().Changed("away")

		// Get list of all rooms to find matches
		listResult, err := myhome.TheClient.CallE(ctx, myhome.TemperatureList, nil)
		if err != nil {
			return fmt.Errorf("failed to list rooms: %w", err)
		}
		allRooms, ok := listResult.(*myhome.TemperatureRoomList)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		// Find matching rooms
		matchingRooms := matchRoomPattern(pattern, *allRooms)

		// If no matches and pattern has no wildcards, treat as new room
		isNewRoom := len(matchingRooms) == 0 && !strings.Contains(pattern, "*")

		if isNewRoom {
			// Creating new room - require all fields
			if !nameSet {
				return fmt.Errorf("--name is required for new room")
			}
			if !kindsSet {
				return fmt.Errorf("--kinds is required for new room (room kinds: bedroom, office, living-room, kitchen, other)")
			}
			if !ecoSet {
				return fmt.Errorf("--eco is required for new room (it's the default temperature level)")
			}

			// Parse kinds
			kinds := parseKinds(kindsStr)
			if len(kinds) == 0 {
				return fmt.Errorf("invalid kinds: %s", kindsStr)
			}

			// Build levels map
			levels := make(map[string]float64)
			levels["eco"] = eco
			if comfortSet && comfort > 0 {
				levels["comfort"] = comfort
			}
			if awaySet && away > 0 {
				levels["away"] = away
			}

			params := &myhome.TemperatureSetParams{
				RoomID: pattern,
				Name:   name,
				Kinds:  kinds,
				Levels: levels,
			}

			result, err := myhome.TheClient.CallE(ctx, myhome.TemperatureSet, params)
			if err != nil {
				return err
			}

			setResult, ok := result.(*myhome.TemperatureSetResult)
			if !ok {
				return fmt.Errorf("unexpected result type")
			}

			fmt.Printf("✓ Temperature configuration saved for room: %s\n", setResult.RoomID)
			return nil
		}

		// Update existing rooms
		if len(matchingRooms) == 0 {
			return fmt.Errorf("no rooms match pattern: %s", pattern)
		}

		updatedCount := 0
		for roomID, existingConfig := range matchingRooms {
			// Build update params - start with existing values
			updateName := existingConfig.Name
			updateKinds := existingConfig.Kinds
			updateLevels := make(map[string]float64)
			for k, v := range existingConfig.Levels {
				updateLevels[k] = v
			}

			// Apply only the flags that were set
			if nameSet {
				updateName = name
			}
			if kindsSet {
				updateKinds = parseKinds(kindsStr)
				if len(updateKinds) == 0 {
					return fmt.Errorf("invalid kinds: %s", kindsStr)
				}
			}
			if ecoSet {
				updateLevels["eco"] = eco
			}
			if comfortSet {
				if comfort > 0 {
					updateLevels["comfort"] = comfort
				} else {
					delete(updateLevels, "comfort")
				}
			}
			if awaySet {
				if away > 0 {
					updateLevels["away"] = away
				} else {
					delete(updateLevels, "away")
				}
			}

			params := &myhome.TemperatureSetParams{
				RoomID: roomID,
				Name:   updateName,
				Kinds:  updateKinds,
				Levels: updateLevels,
			}

			_, err := myhome.TheClient.CallE(ctx, myhome.TemperatureSet, params)
			if err != nil {
				fmt.Printf("✗ Failed to update room %s: %v\n", roomID, err)
				continue
			}

			fmt.Printf("✓ Updated room: %s\n", roomID)
			updatedCount++
		}

		if updatedCount > 0 {
			fmt.Printf("\n✓ Updated %d room(s)\n", updatedCount)
		}
		return nil
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all temperature configurations",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		result, err := myhome.TheClient.CallE(ctx, myhome.TemperatureList, nil)
		if err != nil {
			return err
		}

		rooms, ok := result.(*myhome.TemperatureRoomList)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		if len(*rooms) == 0 {
			fmt.Println("No temperature configurations found")
			return nil
		}

		// Use JSON/YAML output if flag is set
		if options.Flags.Json || cmd.Flags().Changed("output") {
			return options.PrintResult(rooms)
		}

		// Display as table
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ROOM ID\tNAME\tROOM KINDS\tTEMP LEVELS")
		fmt.Fprintln(w, "-------\t----\t----------\t-----------")

		for roomID, config := range *rooms {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				roomID, config.Name, formatKinds(config.Kinds), formatLevels(config.Levels))
		}

		w.Flush()
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete <room-id>",
	Short: "Delete temperature configuration for a room",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		roomID := args[0]

		params := &myhome.TemperatureDeleteParams{
			RoomID: roomID,
		}

		result, err := myhome.TheClient.CallE(ctx, myhome.TemperatureDelete, params)
		if err != nil {
			return err
		}

		deleteResult, ok := result.(*myhome.TemperatureDeleteResult)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		fmt.Printf("✓ Temperature configuration deleted for room: %s\n", deleteResult.RoomID)
		return nil
	},
}

// ============================================================================
// Schedule Commands
// ============================================================================

var scheduleCmd = &cobra.Command{
	Use:   "schedule <room-id> [date]",
	Short: "Get computed temperature schedule for a room",
	Long: `Get the computed temperature schedule for a room on a specific date.
The schedule shows the union of all comfort time ranges for the room's kinds.

Date format: YYYY-MM-DD (defaults to today if not specified)`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		roomID := args[0]

		params := &myhome.TemperatureGetScheduleParams{
			RoomID: roomID,
		}

		// Optional date parameter
		if len(args) > 1 {
			date := args[1]
			params.Date = &date
		}

		result, err := myhome.TheClient.CallE(ctx, myhome.TemperatureGetSchedule, params)
		if err != nil {
			return err
		}

		schedule, ok := result.(*myhome.TemperatureScheduleResult)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		// Display result
		fmt.Printf("Room: %s\n", schedule.RoomID)
		fmt.Printf("Date: %s (%s)\n", schedule.Date, formatWeekday(schedule.Weekday))
		fmt.Printf("Day Type: %s\n", schedule.DayType)
		fmt.Printf("\nTemperature Levels:\n")
		for level, temp := range schedule.Levels {
			fmt.Printf("  %s: %.1f°C\n", level, temp)
		}
		fmt.Printf("\nComfort Time Ranges:\n")
		if len(schedule.ComfortRanges) == 0 {
			fmt.Println("  (always eco)")
		} else {
			for _, tr := range schedule.ComfortRanges {
				fmt.Printf("  %s - %s\n", formatMinutes(tr.Start), formatMinutes(tr.End))
			}
		}

		return nil
	},
}

// ============================================================================
// Weekday Default Commands
// ============================================================================

var weekdayCmd = &cobra.Command{
	Use:   "weekday",
	Short: "Manage global weekday day-type defaults",
	Long: `Manage global weekday day-type defaults (applies to all rooms).

Weekdays: 0=Sunday, 1=Monday, 2=Tuesday, 3=Wednesday, 4=Thursday, 5=Friday, 6=Saturday
Day types: work-day, day-off`,
}

var weekdayGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get global weekday defaults",
	Long: `Get global weekday day-type defaults that apply to all rooms.

Examples:
  myhome ctl temperature weekday get`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		params := &myhome.TemperatureGetWeekdayDefaultsParams{}

		result, err := myhome.TheClient.CallE(ctx, myhome.TemperatureGetWeekdayDefaults, params)
		if err != nil {
			return err
		}

		defaults, ok := result.(*myhome.TemperatureWeekdayDefaults)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		fmt.Printf("Global Weekday Defaults:\n\n")

		// Display in order Sunday-Saturday
		for i := 0; i < 7; i++ {
			dayType, exists := defaults.Defaults[i]
			if !exists {
				dayType = getDefaultDayType(i)
			}
			fmt.Printf("%s: %s\n", formatWeekday(i), dayType)
		}

		return nil
	},
}

var weekdaySetCmd = &cobra.Command{
	Use:   "set <weekday> <day-type>",
	Short: "Set global weekday default",
	Long: `Set the day-type for a specific weekday (applies to all rooms).

Weekdays: 0=Sunday, 1=Monday, 2=Tuesday, 3=Wednesday, 4=Thursday, 5=Friday, 6=Saturday
Day types: work-day, day-off

Examples:
  myhome ctl temperature weekday set 0 day-off    # Sunday is day-off
  myhome ctl temperature weekday set 6 day-off    # Saturday is day-off
  myhome ctl temperature weekday set 1 work-day   # Monday is work-day`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		var weekday int
		_, err := fmt.Sscanf(args[0], "%d", &weekday)
		if err != nil || weekday < 0 || weekday > 6 {
			return fmt.Errorf("invalid weekday: %s (must be 0-6)", args[0])
		}

		dayType := myhome.DayType(args[1])
		if dayType != myhome.DayTypeWorkDay && dayType != myhome.DayTypeDayOff {
			return fmt.Errorf("invalid day-type: %s (must be 'work-day' or 'day-off')", args[1])
		}

		params := &myhome.TemperatureSetWeekdayDefaultParams{
			Weekday: weekday,
			DayType: dayType,
		}

		result, err := myhome.TheClient.CallE(ctx, myhome.TemperatureSetWeekdayDefault, params)
		if err != nil {
			return err
		}

		setResult, ok := result.(*myhome.TemperatureSetWeekdayDefaultResult)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		fmt.Printf("✓ Set %s to %s (global default)\n", formatWeekday(setResult.Weekday), setResult.DayType)
		return nil
	},
}

// ============================================================================
// Kind Schedule Commands
// ============================================================================

var kindScheduleCmd = &cobra.Command{
	Use:   "kind",
	Short: "Manage room kind schedules (comfort time ranges per room kind and day-type)",
	Long: `Manage room kind schedules that define comfort time ranges for each room kind and day type combination.

Room kinds: bedroom, office, living-room, kitchen, other
Day types: work-day, day-off`,
}

var kindScheduleListCmd = &cobra.Command{
	Use:   "list [--kind <kind>] [--day-type <day-type>]",
	Short: "List room kind schedules",
	Long: `List all room kind schedules, optionally filtered by room kind and/or day-type.

Examples:
  myhome ctl temperature kind list
  myhome ctl temperature kind list --kind bedroom
  myhome ctl temperature kind list --day-type work-day
  myhome ctl temperature kind list --kind bedroom --day-type work-day`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		kindStr, _ := cmd.Flags().GetString("kind")
		dayTypeStr, _ := cmd.Flags().GetString("day-type")

		params := &myhome.TemperatureGetKindSchedulesParams{}

		if kindStr != "" {
			kind := myhome.RoomKind(kindStr)
			params.Kind = &kind
		}

		if dayTypeStr != "" {
			dayType := myhome.DayType(dayTypeStr)
			params.DayType = &dayType
		}

		result, err := myhome.TheClient.CallE(ctx, myhome.TemperatureGetKindSchedules, params)
		if err != nil {
			return err
		}

		schedules, ok := result.(*myhome.TemperatureKindScheduleList)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		if len(*schedules) == 0 {
			fmt.Println("No room kind schedules found")
			return nil
		}

		// Display as table
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ROOM KIND\tDAY TYPE\tCOMFORT RANGES")
		fmt.Fprintln(w, "---------\t--------\t--------------")

		for _, sched := range *schedules {
			rangesStr := formatTimeRanges(sched.Ranges)
			fmt.Fprintf(w, "%s\t%s\t%s\n", sched.Kind, sched.DayType, rangesStr)
		}

		w.Flush()
		return nil
	},
}

var kindScheduleSetCmd = &cobra.Command{
	Use:   "set <kind> <day-type> <ranges>",
	Short: "Set comfort time ranges for a room kind and day-type",
	Long: `Set comfort time ranges for a specific room kind and day type combination.

Room kinds: bedroom, office, living-room, kitchen, other
Day types: work-day, day-off
Ranges format: HH:MM-HH:MM or HH:MM-HH:MM,HH:MM-HH:MM for multiple ranges

Examples:
  # Office: comfort during work hours on work days
  myhome ctl temperature kind set office work-day "08:00-18:00"
  
  # Bedroom: comfort in morning and evening on work days
  myhome ctl temperature kind set bedroom work-day "06:00-08:00,20:00-23:00"
  
  # Bedroom: comfort most of the day on days off
  myhome ctl temperature kind set bedroom day-off "08:00-23:00"
  
  # Living room: comfort most of the day
  myhome ctl temperature kind set living-room work-day "06:00-23:00"
  myhome ctl temperature kind set living-room day-off "08:00-23:00"`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		kind := myhome.RoomKind(args[0])
		dayType := myhome.DayType(args[1])
		rangesStr := args[2]

		// Validate room kind
		validKinds := []myhome.RoomKind{
			myhome.RoomKindBedroom,
			myhome.RoomKindOffice,
			myhome.RoomKindLivingRoom,
			myhome.RoomKindKitchen,
			myhome.RoomKindOther,
		}
		if !contains(validKinds, kind) {
			return fmt.Errorf("invalid room kind: %s (valid: bedroom, office, living-room, kitchen, other)", kind)
		}

		// Validate day type
		if dayType != myhome.DayTypeWorkDay && dayType != myhome.DayTypeDayOff {
			return fmt.Errorf("invalid day-type: %s (must be 'work-day' or 'day-off')", dayType)
		}

		// Parse ranges
		ranges := parseScheduleString(rangesStr)
		if len(ranges) == 0 {
			return fmt.Errorf("invalid ranges format: %s", rangesStr)
		}

		params := &myhome.TemperatureSetKindScheduleParams{
			Kind:    kind,
			DayType: dayType,
			Ranges:  ranges,
		}

		result, err := myhome.TheClient.CallE(ctx, myhome.TemperatureSetKindSchedule, params)
		if err != nil {
			return err
		}

		setResult, ok := result.(*myhome.TemperatureSetKindScheduleResult)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		fmt.Printf("✓ Set comfort ranges for room kind '%s' on %s\n", setResult.Kind, setResult.DayType)
		return nil
	},
}

// ============================================================================
// Helper Functions
// ============================================================================

func init() {
	// Set command flags
	setCmd.Flags().String("name", "", "Room name (required)")
	setCmd.Flags().String("kinds", "", "Room kinds, comma-separated (e.g., 'bedroom,office') (required)")
	setCmd.Flags().Float64("eco", 0, "Eco temperature level in °C (required - this is the default)")
	setCmd.Flags().Float64("comfort", 0, "Comfort temperature level in °C (optional)")
	setCmd.Flags().Float64("away", 0, "Away temperature level in °C (optional)")

	// Weekday subcommands
	weekdayCmd.AddCommand(weekdayGetCmd)
	weekdayCmd.AddCommand(weekdaySetCmd)

	// Kind schedule subcommands
	kindScheduleCmd.AddCommand(kindScheduleListCmd)
	kindScheduleCmd.AddCommand(kindScheduleSetCmd)

	// Kind schedule list flags
	kindScheduleListCmd.Flags().String("kind", "", "Filter by room kind (bedroom, office, living-room, kitchen, other)")
	kindScheduleListCmd.Flags().String("day-type", "", "Filter by day-type (work-day, day-off)")
}

func matchRoomPattern(pattern string, rooms myhome.TemperatureRoomList) map[string]*myhome.TemperatureRoomConfig {
	matches := make(map[string]*myhome.TemperatureRoomConfig)

	// Handle wildcard patterns
	if pattern == "*" {
		// Match all rooms
		for roomID, config := range rooms {
			matches[roomID] = config
		}
		return matches
	}

	// Check for prefix wildcard: *suffix
	if strings.HasPrefix(pattern, "*") && !strings.HasSuffix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		for roomID, config := range rooms {
			if strings.HasSuffix(roomID, suffix) {
				matches[roomID] = config
			}
		}
		return matches
	}

	// Check for suffix wildcard: prefix*
	if strings.HasSuffix(pattern, "*") && !strings.HasPrefix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		for roomID, config := range rooms {
			if strings.HasPrefix(roomID, prefix) {
				matches[roomID] = config
			}
		}
		return matches
	}

	// Exact match (no wildcards)
	if config, exists := rooms[pattern]; exists {
		matches[pattern] = config
	}

	return matches
}

func parseKinds(s string) []myhome.RoomKind {
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	kinds := make([]myhome.RoomKind, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			kinds = append(kinds, myhome.RoomKind(part))
		}
	}

	return kinds
}

func formatKinds(kinds []myhome.RoomKind) string {
	if len(kinds) == 0 {
		return "(none)"
	}

	strs := make([]string, len(kinds))
	for i, k := range kinds {
		strs[i] = string(k)
	}
	return strings.Join(strs, ", ")
}

func formatLevels(levels map[string]float64) string {
	if len(levels) == 0 {
		return "(none)"
	}

	// Format as "eco:17.0, comfort:21.0, away:15.0"
	strs := make([]string, 0, len(levels))
	for level, temp := range levels {
		strs = append(strs, fmt.Sprintf("%s:%.1f", level, temp))
	}
	return strings.Join(strs, ", ")
}

func parseScheduleString(s string) []string {
	if s == "" {
		return []string{}
	}

	parts := strings.Split(s, ",")
	ranges := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			ranges = append(ranges, part)
		}
	}

	return ranges
}

func formatTimeRanges(ranges []myhome.TemperatureTimeRange) string {
	if len(ranges) == 0 {
		return "(always eco)"
	}

	strs := make([]string, len(ranges))
	for i, r := range ranges {
		strs[i] = fmt.Sprintf("%s-%s", formatMinutes(r.Start), formatMinutes(r.End))
	}
	return strings.Join(strs, ", ")
}

func formatMinutes(minutes int) string {
	hours := minutes / 60
	mins := minutes % 60
	return fmt.Sprintf("%02d:%02d", hours, mins)
}

func formatWeekday(weekday int) string {
	days := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	if weekday < 0 || weekday > 6 {
		return fmt.Sprintf("Invalid(%d)", weekday)
	}
	return days[weekday]
}

func getDefaultDayType(weekday int) myhome.DayType {
	// Default: Saturday (6) and Sunday (0) are day-off, others are work-day
	if weekday == 0 || weekday == 6 {
		return myhome.DayTypeDayOff
	}
	return myhome.DayTypeWorkDay
}

func contains(kinds []myhome.RoomKind, kind myhome.RoomKind) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}
