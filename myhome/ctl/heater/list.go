package heater

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome/ctl/options"
	"myhome/mqtt"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var listFlags struct {
	Timeout time.Duration
	All     bool
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Discover running heater.js script instances",
	Long: `Send a discovery query via MQTT to find all devices running the heater.js script.
Each device will respond with its configuration and status.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return doList(cmd.Context())
	},
}

func init() {
	listCmd.Flags().DurationVar(&listFlags.Timeout, "timeout", 5*time.Second, "Discovery timeout")
	listCmd.Flags().BoolVar(&listFlags.All, "all", false, "Show full configuration and state details")
}

// HeaterDiscoveryResponse represents the response from a heater.js instance
type HeaterDiscoveryResponse struct {
	DeviceID   string                 `json:"device_id" yaml:"device_id"`
	DeviceName string                 `json:"device_name" yaml:"device_name"`
	ScriptName string                 `json:"script_name" yaml:"script_name"`
	Config     map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
	State      map[string]interface{} `json:"state,omitempty" yaml:"state,omitempty"`
	Timestamp  float64                `json:"timestamp,omitempty" yaml:"timestamp,omitempty"`
}

// HeaterSummary is a simplified view of a heater instance
type HeaterSummary struct {
	DeviceID   string `json:"device_id" yaml:"device_id"`
	DeviceName string `json:"device_name" yaml:"device_name"`
	ScriptName string `json:"script_name" yaml:"script_name"`
}

func doList(ctx context.Context) error {
	log := hlog.Logger

	// Get MQTT client
	mc, err := mqtt.GetClientE(ctx)
	if err != nil {
		return fmt.Errorf("failed to get MQTT client: %w", err)
	}

	// Build response topic with CLI client ID
	clientId := mc.Id()
	responseTopic := fmt.Sprintf("myhome/heater/list/response/%s", clientId)

	log.Info("Subscribing to list responses", "topic", responseTopic)

	responsesChan, err := mc.Subscriber(ctx, responseTopic, 10)
	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", responseTopic, err)
	}

	// Wait a bit for subscription to be established
	time.Sleep(100 * time.Millisecond)

	// Publish list query
	queryTopic := "myhome/heater/list"
	log.Info("Publishing list query", "topic", queryTopic)

	queryPayload := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"replyTo":   responseTopic,
	}
	queryBytes, _ := json.Marshal(queryPayload)

	if err := mc.Publish(ctx, queryTopic, queryBytes); err != nil {
		return fmt.Errorf("failed to publish discovery query: %w", err)
	}

	// Collect responses with timeout
	log.Info("Discovering heater instances", "timeout", listFlags.Timeout)

	discovered := make(map[string]HeaterDiscoveryResponse)
	timeout := time.After(listFlags.Timeout)

	for {
		select {
		case payload := <-responsesChan:
			var resp HeaterDiscoveryResponse
			if err := json.Unmarshal(payload, &resp); err != nil {
				log.Error(err, "Failed to parse discovery response", "payload", string(payload))
				continue
			}
			discovered[resp.DeviceID] = resp
			log.Info("Found heater instance", "device", resp.DeviceName, "id", resp.DeviceID)

		case <-timeout:
			// Format and display results
			return formatOutput(discovered)

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func formatOutput(discovered map[string]HeaterDiscoveryResponse) error {
	if len(discovered) == 0 {
		// Empty list
		if options.Flags.Json {
			fmt.Println("[]")
		} else {
			fmt.Println("[]")
		}
		return nil
	}

	var output interface{}

	if listFlags.All {
		// Full details: convert map to slice
		results := make([]HeaterDiscoveryResponse, 0, len(discovered))
		for _, resp := range discovered {
			results = append(results, resp)
		}
		output = results
	} else {
		// Summary only: just device info
		summaries := make([]HeaterSummary, 0, len(discovered))
		for _, resp := range discovered {
			summaries = append(summaries, HeaterSummary{
				DeviceID:   resp.DeviceID,
				DeviceName: resp.DeviceName,
				ScriptName: resp.ScriptName,
			})
		}
		output = summaries
	}

	var outputBytes []byte
	var err error

	if options.Flags.Json {
		outputBytes, err = json.MarshalIndent(output, "", "  ")
	} else {
		outputBytes, err = yaml.Marshal(output)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	fmt.Println(string(outputBytes))
	return nil
}
