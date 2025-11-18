package temperature

import (
	"context"
	"encoding/json"
	"fmt"
	"myhome"
	"myhome/ctl/options"
	"myhome/mqtt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "temperature",
	Short: "Manage temperature configurations",
	Long:  `Manage room temperature configurations including comfort/eco temperatures and schedules.`,
}

func init() {
	Cmd.AddCommand(getCmd)
	Cmd.AddCommand(setCmd)
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(deleteCmd)
	Cmd.AddCommand(setpointCmd)
}

var getCmd = &cobra.Command{
	Use:   "get <room-id>",
	Short: "Get temperature configuration for a room",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log, err := logr.FromContext(ctx)
		if err != nil {
			return err
		}

		roomID := args[0]

		// Call RPC method
		params := &myhome.TemperatureGetParams{
			RoomID: roomID,
		}

		result, err := callRPC(ctx, log, myhome.TemperatureGet, params)
		if err != nil {
			return err
		}

		config, ok := result.(*myhome.TemperatureRoomConfig)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		// Display result
		fmt.Printf("Room: %s (%s)\n", config.Name, config.RoomID)
		fmt.Printf("Comfort Temperature: %.1f°C\n", config.ComfortTemp)
		fmt.Printf("Eco Temperature: %.1f°C\n", config.EcoTemp)
		fmt.Printf("\nWeekday Schedule (Comfort Hours):\n")
		for _, tr := range config.Schedule.Weekday {
			fmt.Printf("  %s - %s\n", tr.Start, tr.End)
		}
		fmt.Printf("\nWeekend Schedule (Comfort Hours):\n")
		for _, tr := range config.Schedule.Weekend {
			fmt.Printf("  %s - %s\n", tr.Start, tr.End)
		}

		return nil
	},
}

var setCmd = &cobra.Command{
	Use:   "set <room-id>",
	Short: "Set temperature configuration for a room",
	Long: `Set or update temperature configuration for a room.
	
Examples:
  # Set basic configuration
  myhome ctl temperature set living-room --name "Living Room" --comfort 21 --eco 17
  
  # Set with schedule
  myhome ctl temperature set living-room --name "Living Room" --comfort 21 --eco 17 \
    --weekday "06:00-23:00" --weekend "08:00-23:00"
  
  # Multiple time ranges
  myhome ctl temperature set bedroom --name "Bedroom" --comfort 19 --eco 16 \
    --weekday "06:00-08:00,20:00-23:00" --weekend "08:00-23:00"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log, err := logr.FromContext(ctx)
		if err != nil {
			return err
		}

		roomID := args[0]
		name, _ := cmd.Flags().GetString("name")
		comfort, _ := cmd.Flags().GetFloat64("comfort")
		eco, _ := cmd.Flags().GetFloat64("eco")
		weekday, _ := cmd.Flags().GetString("weekday")
		weekend, _ := cmd.Flags().GetString("weekend")

		// Validate required flags
		if name == "" {
			return fmt.Errorf("--name is required")
		}
		if comfort == 0 {
			return fmt.Errorf("--comfort is required")
		}
		if eco == 0 {
			return fmt.Errorf("--eco is required")
		}

		// Parse schedules
		weekdayRanges := parseScheduleString(weekday)
		weekendRanges := parseScheduleString(weekend)

		// Call RPC method
		params := &myhome.TemperatureSetParams{
			RoomID:      roomID,
			Name:        name,
			ComfortTemp: comfort,
			EcoTemp:     eco,
			Weekday:     weekdayRanges,
			Weekend:     weekendRanges,
		}

		result, err := callRPC(ctx, log, myhome.TemperatureSet, params)
		if err != nil {
			return err
		}

		setResult, ok := result.(*myhome.TemperatureSetResult)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		fmt.Printf("✓ Temperature configuration saved for room: %s\n", setResult.RoomID)
		return nil
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all temperature configurations",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log, err := logr.FromContext(ctx)
		if err != nil {
			return err
		}

		// Call RPC method
		result, err := callRPC(ctx, log, myhome.TemperatureList, nil)
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

		// Display as table
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ROOM ID\tNAME\tCOMFORT\tECO\tWEEKDAY SCHEDULE\tWEEKEND SCHEDULE")
		fmt.Fprintln(w, "-------\t----\t-------\t---\t----------------\t----------------")

		for roomID, config := range *rooms {
			weekdayStr := formatSchedule(config.Schedule.Weekday)
			weekendStr := formatSchedule(config.Schedule.Weekend)
			fmt.Fprintf(w, "%s\t%s\t%.1f°C\t%.1f°C\t%s\t%s\n",
				roomID, config.Name, config.ComfortTemp, config.EcoTemp,
				weekdayStr, weekendStr)
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
		log, err := logr.FromContext(ctx)
		if err != nil {
			return err
		}

		roomID := args[0]

		// Call RPC method
		params := &myhome.TemperatureDeleteParams{
			RoomID: roomID,
		}

		result, err := callRPC(ctx, log, myhome.TemperatureDelete, params)
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

var setpointCmd = &cobra.Command{
	Use:   "setpoint <room-id>",
	Short: "Get current temperature setpoint for a room",
	Long:  `Get the current active temperature setpoint based on the current time and schedule.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log, err := logr.FromContext(ctx)
		if err != nil {
			return err
		}

		roomID := args[0]

		// Call RPC method
		params := &myhome.TemperatureGetSetpointParams{
			RoomID: roomID,
		}

		result, err := callRPC(ctx, log, myhome.TemperatureSetpoint, params)
		if err != nil {
			return err
		}

		setpoint, ok := result.(*myhome.TemperatureSetpointResult)
		if !ok {
			return fmt.Errorf("unexpected result type")
		}

		// Display result
		fmt.Printf("Room: %s\n", roomID)
		fmt.Printf("Current Time: %s\n", time.Now().Format("15:04"))
		fmt.Printf("\nActive Setpoint: %.1f°C (%s)\n", setpoint.ActiveSetpoint, setpoint.Reason)
		fmt.Printf("Comfort Setpoint: %.1f°C\n", setpoint.SetpointComfort)
		fmt.Printf("Eco Setpoint: %.1f°C\n", setpoint.SetpointEco)

		return nil
	},
}

func init() {
	// Set command flags
	setCmd.Flags().String("name", "", "Room name (required)")
	setCmd.Flags().Float64("comfort", 0, "Comfort temperature in °C (required)")
	setCmd.Flags().Float64("eco", 0, "Eco temperature in °C (required)")
	setCmd.Flags().String("weekday", "", "Weekday comfort hours (e.g., '06:00-23:00' or '06:00-08:00,20:00-23:00')")
	setCmd.Flags().String("weekend", "", "Weekend comfort hours (e.g., '08:00-23:00')")
}

// callRPC calls a MyHome RPC method
func callRPC(ctx context.Context, log logr.Logger, method myhome.Verb, params any) (any, error) {
	mc, err := mqtt.GetClientE(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get MQTT client: %w", err)
	}

	// Create RPC request
	req := struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{
		ID:     fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Method: string(method),
		Params: params,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Subscribe to response topic
	replyTopic := fmt.Sprintf("myhome/rpc/reply/%s", req.ID)
	replyChan, err := mc.Subscriber(ctx, replyTopic, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to reply topic: %w", err)
	}

	// Publish request
	if err := mc.Publish(ctx, "myhome/rpc", reqBytes); err != nil {
		return nil, fmt.Errorf("failed to publish request: %w", err)
	}

	log.Info("RPC request sent", "method", method, "id", req.ID)

	// Wait for response with timeout
	timeout := time.After(options.Flags.MqttTimeout)
	select {
	case respBytes := <-replyChan:
		var resp struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}

		if err := json.Unmarshal(respBytes, &resp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		// Get method signature to unmarshal result
		methodDef, err := myhome.Methods(method)
		if err != nil {
			return nil, err
		}

		result := methodDef.Signature.NewResult()
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal result: %w", err)
		}

		return result, nil

	case <-timeout:
		return nil, fmt.Errorf("RPC timeout waiting for response")

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// parseScheduleString parses a schedule string like "06:00-23:00" or "06:00-08:00,20:00-23:00"
func parseScheduleString(s string) []string {
	if s == "" {
		return []string{}
	}

	// Split by comma for multiple ranges
	var ranges []string
	for _, part := range splitByComma(s) {
		part = trimSpace(part)
		if part != "" {
			ranges = append(ranges, part)
		}
	}

	return ranges
}

// formatSchedule formats a schedule for display
func formatSchedule(ranges []myhome.TemperatureTimeRange) string {
	if len(ranges) == 0 {
		return "(always eco)"
	}

	result := ""
	for i, r := range ranges {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%s-%s", r.Start, r.End)
	}
	return result
}

// Helper functions for string manipulation (ES5-compatible approach)
func splitByComma(s string) []string {
	var result []string
	current := ""
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			result = append(result, current)
			current = ""
		} else {
			current += string(s[i])
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)

	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}

	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}

	return s[start:end]
}
