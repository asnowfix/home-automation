# Shelly Device Data Collector

A comprehensive data collection tool for capturing API interactions with all known local Shelly device types. This tool is designed to create test data for non-regression testing of the Shelly device API integration.

## Overview

The data collector automatically discovers all Shelly devices on the local network and systematically calls their known API methods, recording the exact format of messages sent and received. This data is then stored in JSON format for use in automated testing suites.

## Design & Architecture

### Core Components

1. **Device Discovery**: Uses the existing home automation client (`internal/myhome`) to enumerate all local Shelly devices
2. **API Method Testing**: Systematically calls a comprehensive list of known API methods for each device
3. **Data Capture**: Records request/response pairs with metadata (timestamps, device info, errors)
4. **JSON Export**: Saves collected data in structured JSON format for test suite consumption

### API Methods Tested

The collector tests the following categories of API methods:

#### System APIs
- `Sys.GetConfig` - System configuration
- `Shelly.GetDeviceInfo` - Device information and capabilities
- `Shelly.GetStatus` - Overall device status
- `Shelly.GetConfig` - Device configuration
- `Shelly.ListMethods` - Available RPC methods

#### Network APIs
- `WiFi.GetStatus` - WiFi connection status
- `WiFi.GetConfig` - WiFi configuration
- `Eth.GetConfig` - Ethernet configuration
- `Eth.GetStatus` - Ethernet status

#### Component APIs
- `Switch.GetConfig` / `Switch.GetStatus` - Switch component (ID 0)
- `Input.GetConfig` / `Input.GetStatus` - Input component (ID 0)
- `MQTT.GetConfig` / `MQTT.GetStatus` - MQTT configuration and status

#### Storage & Scripting APIs
- `KVS.GetMany` - Key-value store data (wildcard query)
- `Script.List` - Installed scripts

### Data Structure

Each API call is recorded with the following structure:

```json
{
  "timestamp": "2025-08-01T23:39:03.123Z",
  "device_id": "shellyplus1-441793d69718",
  "device_name": "Shelly Plus 1 Kitchen",
  "device_model": "SNSW-001X16EU",
  "method": "Shelly.GetStatus",
  "channel": "default",
  "request": null,
  "response": { /* actual response data */ },
  "error": "",
  "duration": "45.2ms"
}
```

The complete test suite includes:

```json
{
  "collection_time": "2025-08-01T23:39:03.123Z",
  "version": "1.0.0",
  "api_calls": [ /* array of API calls */ ],
  "device_types": ["shellyplus1", "shellypro4pm", ...],
  "summary": {
    "total_calls": 85,
    "successful_calls": 82,
    "failed_calls": 3,
    "device_count": 5
  }
}
```

## Usage

### Prerequisites

- Go 1.23+ installed
- Access to the home automation MQTT broker
- Local network with discoverable Shelly devices

### Building

```bash
cd cmd/datacollector
go build -o datacollector .
```

### Running

```bash
./datacollector
```

The tool will:
1. Initialize logging and MQTT connection
2. Discover all local Shelly devices
3. Test each device with all known API methods
4. Save results to `test_data/shelly_api_test_data_YYYYMMDD_HHMMSS.json`

### Output

Results are saved in the `test_data/` directory with timestamped filenames:
- `shelly_api_test_data_20250801_233903.json`

Each file contains the complete test suite data ready for use in non-regression tests.

## Implementation Details

### Error Handling

- **Network Errors**: Captured and recorded in the `error` field
- **API Errors**: Shelly device error responses are captured in the response data
- **Timeout Handling**: 100ms delay between API calls to avoid overwhelming devices
- **Graceful Degradation**: Continues testing other methods/devices if individual calls fail

### Logging

Uses the existing `hlog` package for structured logging:
- Info level: Device discovery and progress
- Error level: Connection failures and critical errors
- Debug level: Individual API call details (verbose mode)

### Concurrency

- **Thread Safety**: Uses existing `sync.Map` implementation for dialog tracking
- **Sequential Processing**: Processes devices and API calls sequentially to avoid overwhelming the network
- **Timeout Management**: 30-second timeout for overall operations

### Dependencies

The collector integrates with existing home automation infrastructure:

- `internal/myhome` - Device discovery and management
- `pkg/shelly` - Shelly device API implementation
- `mymqtt` - MQTT communication layer
- `hlog` - Structured logging
- `homectl/options` - Context and configuration management

## Testing Strategy

### Non-Regression Testing

The collected data serves as a baseline for:

1. **API Compatibility**: Ensuring new code changes don't break existing device communication
2. **Response Format Validation**: Verifying response structures remain consistent
3. **Error Handling**: Testing error scenarios and edge cases
4. **Performance Regression**: Monitoring API call duration trends

### Test Data Usage

The JSON output can be used in automated tests:

```go
// Example test usage
func TestShellyAPICompatibility(t *testing.T) {
    testData := loadTestData("shelly_api_test_data_20250801_233903.json")
    
    for _, apiCall := range testData.APICalls {
        // Replay the API call and compare responses
        response := replayAPICall(apiCall)
        assert.JSONEq(t, apiCall.Response, response)
    }
}
```

## Troubleshooting

### Common Issues

1. **MQTT Broker Not Found**
   - Ensure the MQTT broker is running and discoverable
   - Check network connectivity and mDNS resolution

2. **Device Discovery Fails**
   - Verify devices are powered on and connected to the network
   - Check that devices are in the same network segment

3. **API Call Timeouts**
   - Increase timeout values if network is slow
   - Reduce concurrent operations

### Debug Mode

For detailed debugging, modify the logger initialization:

```go
hlog.Init(true) // Enable verbose logging
```

This will show detailed API call traces and network operations.

## Future Enhancements

- **Parallel Processing**: Add concurrent device testing with rate limiting
- **Configuration File**: Support for custom API method lists and test parameters
- **Comparison Mode**: Compare current results against baseline data
- **Device Filtering**: Support for testing specific device types or IDs
- **Continuous Monitoring**: Scheduled data collection for trend analysis

## Contributing

When adding new API methods to test:

1. Add the method to the `methods` slice in `testDeviceAPIs()`
2. Ensure proper error handling and parameter passing
3. Update this documentation with the new method details
4. Test with actual devices before committing

## License

This tool is part of the home automation project and follows the same licensing terms.
