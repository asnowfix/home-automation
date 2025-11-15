package script

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-logr/logr"
)

// DeviceState represents the persistent state of a Shelly device for testing
type DeviceState struct {
	KVS     map[string]interface{} `json:"kvs"`
	Storage map[string]interface{} `json:"storage"`
}

// GetKVS implements the DeviceState interface
func (d *DeviceState) GetKVS() map[string]interface{} {
	return d.KVS
}

// GetStorage implements the DeviceState interface
func (d *DeviceState) GetStorage() map[string]interface{} {
	return d.Storage
}

// LoadDeviceState loads device state from a JSON file
// If the file doesn't exist, returns an empty state
func LoadDeviceState(log logr.Logger, filename string) (*DeviceState, error) {
	state := &DeviceState{
		KVS:     make(map[string]interface{}),
		Storage: make(map[string]interface{}),
	}

	// If file doesn't exist, return empty state
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		log.Info("Device state file not found, starting with empty state", "file", filename)
		return state, nil
	}

	// Read file
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read device state file: %w", err)
	}

	// Parse JSON
	err = json.Unmarshal(data, state)
	if err != nil {
		return nil, fmt.Errorf("failed to parse device state file: %w", err)
	}

	// Ensure maps are initialized
	if state.KVS == nil {
		state.KVS = make(map[string]interface{})
	}
	if state.Storage == nil {
		state.Storage = make(map[string]interface{})
	}

	log.Info("Loaded device state", "file", filename, "kvs_keys", len(state.KVS), "storage_keys", len(state.Storage))
	return state, nil
}

// SaveDeviceState saves device state to a JSON file with indentation
func SaveDeviceState(log logr.Logger, filename string, state *DeviceState) error {
	// Marshal with indentation
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal device state: %w", err)
	}

	// Write to file
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write device state file: %w", err)
	}

	log.Info("Saved device state", "file", filename, "kvs_keys", len(state.KVS), "storage_keys", len(state.Storage))
	return nil
}

// RunWithDeviceFile runs a Shelly script with device state persistence
func RunWithDeviceFile(ctx context.Context, name string, buf []byte, minify bool, deviceFile string) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	// Load device state if file specified
	var deviceState *DeviceState
	if deviceFile != "" {
		deviceState, err = LoadDeviceState(log, deviceFile)
		if err != nil {
			log.Error(err, "Failed to load device state", "file", deviceFile)
			return err
		}
	} else {
		// Empty state
		deviceState = &DeviceState{
			KVS:     make(map[string]interface{}),
			Storage: make(map[string]interface{}),
		}
	}

	// Run the script with device state
	err = RunWithDeviceState(ctx, name, buf, minify, deviceState)
	
	// Save device state after script completes
	if deviceFile != "" && err != nil && err != context.Canceled {
		// Only save if there was no error or if it was a cancellation
		return err
	}
	
	if deviceFile != "" {
		if saveErr := SaveDeviceState(log, deviceFile, deviceState); saveErr != nil {
			log.Error(saveErr, "Failed to save device state", "file", deviceFile)
			if err == nil {
				err = saveErr
			}
		}
	}
	
	return err
}
