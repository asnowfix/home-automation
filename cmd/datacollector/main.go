package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"internal/global"
	"internal/myhome"
	"pkg/shelly"
	"pkg/shelly/ethernet"
	"pkg/shelly/kvs"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"

	"hlog"
	"homectl/options"

	"github.com/go-logr/logr"
)

// APICall represents a single API call with request and response
type APICall struct {
	Timestamp   time.Time   `json:"timestamp"`
	DeviceID    string      `json:"device_id"`
	DeviceName  string      `json:"device_name"`
	DeviceModel string      `json:"device_model,omitempty"`
	Method      string      `json:"method"`
	Channel     string      `json:"channel"`
	Request     interface{} `json:"request"`
	Response    interface{} `json:"response"`
	Error       string      `json:"error,omitempty"`
	Duration    string      `json:"duration"`
}

// TestSuite represents the complete collection of API calls
type TestSuite struct {
	CollectionTime time.Time `json:"collection_time"`
	Version        string    `json:"version"`
	APICalls       []APICall `json:"api_calls"`
	DeviceTypes    []string  `json:"device_types"`
	Summary        struct {
		TotalCalls      int `json:"total_calls"`
		SuccessfulCalls int `json:"successful_calls"`
		FailedCalls     int `json:"failed_calls"`
		DeviceCount     int `json:"device_count"`
	} `json:"summary"`
}

var logger logr.Logger

var Version string = "dirty"

func main() {
	// Initialize logger
	hlog.Init(false) // not verbose
	logger = hlog.Logger

	ctx := context.Background()
	ctx = options.CommandLineContext(ctx, logger, 30*time.Second, Version)

	// Debug: Check if logger is in context
	if logFromCtx, ok := ctx.Value(global.LogKey).(logr.Logger); ok {
		fmt.Printf("Logger successfully added to context: %T\n", logFromCtx)
	} else {
		fmt.Printf("Failed to add logger to context\n")
	}

	// Initialize the home automation client
	client, err := myhome.NewClientE(ctx, logger, 30*time.Second)
	if err != nil {
		log.Fatalf("Failed to create myhome client: %v", err)
	}
	myhome.TheClient = client

	// Get all devices
	devices, err := myhome.TheClient.LookupDevices(ctx, "*")
	if err != nil {
		log.Fatalf("Failed to get devices: %v", err)
	}

	logger.Info("Starting data collection", "device_count", len(*devices))

	testSuite := &TestSuite{
		CollectionTime: time.Now(),
		Version:        "1.0.0",
		APICalls:       make([]APICall, 0),
		DeviceTypes:    make([]string, 0),
	}

	deviceTypes := make(map[string]bool)

	// Test each device
	for _, device := range *devices {
		logger.Info("Testing device", "id", device.Id(), "name", device.Name())

		// Create Shelly device instance
		shellyDevice, err := shelly.NewDeviceFromSummary(ctx, logger, device)
		if err != nil {
			logger.Error(err, "Failed to create Shelly device", "device_id", device.Id())
			continue
		}

		sd, ok := shellyDevice.(*shelly.Device)
		if !ok {
			logger.Error(fmt.Errorf("not a Shelly device"), "Invalid device type", "device_id", device.Id())
			continue
		}

		// Test core API methods for this device
		testDeviceAPIs(ctx, sd, testSuite)

		// Track device types (simplified)
		deviceTypes[device.Id()[:strings.Index(device.Id(), "-")]] = true
	}

	// Populate device types
	for deviceType := range deviceTypes {
		testSuite.DeviceTypes = append(testSuite.DeviceTypes, deviceType)
	}

	// Calculate summary
	testSuite.Summary.TotalCalls = len(testSuite.APICalls)
	testSuite.Summary.DeviceCount = len(*devices)
	for _, call := range testSuite.APICalls {
		if call.Error == "" {
			testSuite.Summary.SuccessfulCalls++
		} else {
			testSuite.Summary.FailedCalls++
		}
	}

	// Save to JSON file
	outputFile := fmt.Sprintf("shelly_api_test_data_%s.json", time.Now().Format("20060102_150405"))
	err = saveTestSuite(testSuite, outputFile)
	if err != nil {
		log.Fatalf("Failed to save test suite: %v", err)
	}

	logger.Info("Data collection completed",
		"output_file", outputFile,
		"total_calls", testSuite.Summary.TotalCalls,
		"successful_calls", testSuite.Summary.SuccessfulCalls,
		"failed_calls", testSuite.Summary.FailedCalls,
		"device_types", testSuite.DeviceTypes)
}

func testDeviceAPIs(ctx context.Context, device *shelly.Device, testSuite *TestSuite) {
	// Define core API methods to test (only ones we know exist)
	methods := []struct {
		name   string
		caller func() (interface{}, error)
	}{
		{
			name: "Sys.GetConfig",
			caller: func() (interface{}, error) {
				return system.GetConfig(ctx, device)
			},
		},
		{
			name: "Shelly.GetDeviceInfo",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "Shelly.GetDeviceInfo", nil)
			},
		},
		{
			name: "WiFi.GetStatus",
			caller: func() (interface{}, error) {
				return wifi.DoGetStatus(ctx, types.ChannelDefault, device)
			},
		},
		{
			name: "WiFi.GetConfig",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "WiFi.GetConfig", nil)
			},
		},
		{
			name: "Eth.GetConfig",
			caller: func() (interface{}, error) {
				return ethernet.GetConfig(ctx, device, types.ChannelDefault)
			},
		},
		{
			name: "Eth.GetStatus",
			caller: func() (interface{}, error) {
				return ethernet.GetStatus(ctx, device, types.ChannelDefault)
			},
		},
		{
			name: "KVS.GetMany",
			caller: func() (interface{}, error) {
				return kvs.GetManyValues(ctx, logger, types.ChannelDefault, device, "*")
			},
		},
		{
			name: "Shelly.GetStatus",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "Shelly.GetStatus", nil)
			},
		},
		{
			name: "Shelly.GetConfig",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "Shelly.GetConfig", nil)
			},
		},
		{
			name: "Shelly.ListMethods",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "Shelly.ListMethods", nil)
			},
		},
		{
			name: "Switch.GetConfig",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "Switch.GetConfig", map[string]interface{}{"id": 0})
			},
		},
		{
			name: "Switch.GetStatus",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "Switch.GetStatus", map[string]interface{}{"id": 0})
			},
		},
		{
			name: "Input.GetConfig",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "Input.GetConfig", map[string]interface{}{"id": 0})
			},
		},
		{
			name: "Input.GetStatus",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "Input.GetStatus", map[string]interface{}{"id": 0})
			},
		},
		{
			name: "MQTT.GetConfig",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "MQTT.GetConfig", nil)
			},
		},
		{
			name: "MQTT.GetStatus",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "MQTT.GetStatus", nil)
			},
		},
		{
			name: "Script.List",
			caller: func() (interface{}, error) {
				return device.CallE(ctx, types.ChannelDefault, "Script.List", nil)
			},
		},
	}

	// Test each method
	for _, method := range methods {
		start := time.Now()

		call := APICall{
			Timestamp:  start,
			DeviceID:   device.Id(),
			DeviceName: device.Name(),
			Method:     method.name,
			Channel:    "default",
			Request:    nil,
		}

		response, err := method.caller()
		call.Duration = time.Since(start).String()

		if err != nil {
			call.Error = err.Error()
			logger.V(1).Info("API call failed", "device_id", device.Id(), "method", method.name, "error", err)
		} else {
			call.Response = response
			logger.V(2).Info("API call succeeded", "device_id", device.Id(), "method", method.name)
		}

		testSuite.APICalls = append(testSuite.APICalls, call)

		// Small delay between calls to avoid overwhelming the device
		time.Sleep(100 * time.Millisecond)
	}
}

func saveTestSuite(testSuite *TestSuite, filename string) error {
	// Create output directory if it doesn't exist
	outputDir := "test_data"
	err := os.MkdirAll(outputDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	filePath := filepath.Join(outputDir, filename)

	// Marshal to JSON with pretty printing
	jsonData, err := json.MarshalIndent(testSuite, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal test suite: %v", err)
	}

	// Write to file
	err = os.WriteFile(filePath, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write file: %v", err)
	}

	logger.Info("Test data saved", "file", filePath, "size_bytes", len(jsonData))
	return nil
}
