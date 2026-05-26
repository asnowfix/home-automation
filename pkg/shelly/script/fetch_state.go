package script

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	shellyrpc "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
)

// FetchDeviceState fetches the live state of a Shelly device and returns a
// DeviceState suitable for use with RunWithDeviceState or SaveDeviceState.
//
// Fetched fields:
//   - KVS: all key-value pairs stored on the device
//   - ComponentStatus: point-in-time snapshot of all component statuses
//   - Storage: always empty (Script.storage is not accessible via RPC)
func FetchDeviceState(ctx context.Context, via types.Channel, device types.Device) (*DeviceState, error) {
	kvsData, err := fetchAllKVS(ctx, via, device)
	if err != nil {
		return nil, fmt.Errorf("fetching KVS: %w", err)
	}

	componentStatus, err := fetchComponentStatus(ctx, via, device)
	if err != nil {
		return nil, fmt.Errorf("fetching component status: %w", err)
	}

	return &DeviceState{
		KVS:             kvsData,
		Storage:         make(map[string]interface{}),
		ComponentStatus: componentStatus,
	}, nil
}

// fetchAllKVS retrieves all KVS entries from the device.
// It uses KVS.List to enumerate key names and then KVS.Get for each key
// individually. KVS.GetMany with an empty match pattern returns null values
// on some firmware versions, so the List+Get approach is more reliable.
func fetchAllKVS(ctx context.Context, via types.Channel, device types.Device) (map[string]interface{}, error) {
	// Step 1: list all key names.
	out, err := device.CallE(ctx, via, kvs.List.String(), &kvs.ListOrGetManyRequest{Match: "*"})
	if err != nil {
		return nil, fmt.Errorf("KVS.List: %w", err)
	}
	listResp, ok := out.(*kvs.ListResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected KVS.List response type %T", out)
	}

	// Step 2: fetch each value individually.
	result := make(map[string]interface{}, len(listResp.Keys))
	for key := range listResp.Keys {
		out, err := device.CallE(ctx, via, kvs.Get.String(), &kvs.GetRequest{Key: key})
		if err != nil {
			return nil, fmt.Errorf("KVS.Get %q: %w", key, err)
		}
		getResp, ok := out.(*kvs.GetResponse)
		if !ok {
			return nil, fmt.Errorf("unexpected KVS.Get response type %T", out)
		}
		// KVS values are always stored as plain strings on Shelly devices.
		// Store them as-is without JSON parsing to preserve the string type.
		result[key] = getResp.Value
	}
	return result, nil
}

// fetchComponentStatus retrieves the current status of all device components.
// The returned map uses component keys such as "switch:0", "sys", "mqtt", etc.
// matching the keys expected by Shelly.getComponentStatus() in the local VM.
func fetchComponentStatus(ctx context.Context, via types.Channel, device types.Device) (map[string]interface{}, error) {
	out, err := device.CallE(ctx, via, shellyrpc.GetStatus.String(), nil)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return make(map[string]interface{}), nil
	}
	// Marshal the typed Status struct to JSON, then unmarshal to a flat map.
	// This gives the right key shape: {"switch:0": {...}, "sys": {...}, ...}
	data, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("marshaling status: %w", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshaling status: %w", err)
	}
	return m, nil
}
