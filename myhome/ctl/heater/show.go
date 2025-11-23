package heater

import (
	"context"
	"encoding/json"
	"fmt"
	"hlog"
	"myhome"
	"myhome/ctl/options"
	"myhome/mqtt"
	"pkg/devices"
	"pkg/shelly/types"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var showFlags struct {
	Timeout time.Duration
}

var showCmd = &cobra.Command{
	Use:   "show <device>",
	Short: "Show heater configuration and state for a specific device",
	Long:  `Query a specific device running heater.js for its configuration and state.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		device := args[0]
		_, err := myhome.Foreach(cmd.Context(), hlog.Logger, device, options.Via, doShow, nil)
		return err
	},
}

func init() {
	showCmd.Flags().DurationVar(&showFlags.Timeout, "timeout", 5*time.Second, "Query timeout")
}

func doShow(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	// Get MQTT client
	mc, err := mqtt.GetClientE(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get MQTT client: %w", err)
	}

	// Build response topic with device ID and CLI client ID
	clientId := mc.Id()
	responseTopic := fmt.Sprintf("myhome/heater/show/response/%s/%s", device.Id(), clientId)

	log.Info("Subscribing to response", "topic", responseTopic)

	responsesChan, err := mc.Subscriber(ctx, responseTopic, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to %s: %w", responseTopic, err)
	}

	// Wait a bit for subscription to be established
	time.Sleep(100 * time.Millisecond)

	// Publish show query to device-specific topic
	queryTopic := fmt.Sprintf("myhome/heater/show/%s", device.Id())
	log.Info("Publishing show query", "topic", queryTopic, "device", device.Id())

	queryPayload := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"replyTo":   responseTopic,
	}
	queryBytes, _ := json.Marshal(queryPayload)

	if err := mc.Publish(ctx, queryTopic, queryBytes); err != nil {
		return nil, fmt.Errorf("failed to publish show query: %w", err)
	}

	// Wait for response with timeout
	log.Info("Waiting for response", "timeout", showFlags.Timeout)

	timeout := time.After(showFlags.Timeout)

	select {
	case payload := <-responsesChan:
		var resp HeaterDiscoveryResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		// Format output
		var outputBytes []byte
		if options.Flags.Json {
			outputBytes, err = json.MarshalIndent(resp, "", "  ")
		} else {
			outputBytes, err = yaml.Marshal(resp)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to marshal output: %w", err)
		}

		fmt.Println(string(outputBytes))
		return nil, nil

	case <-timeout:
		return nil, fmt.Errorf("timeout waiting for response from %s", device.Name())

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
