package ble

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/BLE

// Config represents the BLE configuration for the device.
type Config struct {
	Enable   bool        `json:"enable"`             // True if bluetooth is enabled, false otherwise
	RPC      *RPCConfig  `json:"rpc,omitempty"`      // Configuration of the rpc service
	Observer *Observer   `json:"observer,omitempty"` // Configuration of the BT LE observer (obsoleted as of 1.5.0-beta1)
}

// RPCConfig represents the RPC service configuration for BLE
type RPCConfig struct {
	Enable      bool `json:"enable"`                 // True if rpc service is enabled, false otherwise
	KeepRunning bool `json:"keep_running,omitempty"` // When true, changing config requires restart (as of 1.6.0-beta1)
}

// Observer represents the BT LE observer configuration
// Obsoleted as of 1.5.0-beta1 - observer functionality is now automatic using Enhanced Scan Manager
type Observer struct {
	Enable bool `json:"enable"` // True if BT LE observer is enabled, false otherwise. Not applicable for battery-operated devices.
}

// Status represents the BLE component status
type Status struct {
	BluTrvAssoc *BluTrvAssociation `json:"blutrv_assoc,omitempty"` // BluTrvAssociations information, present only when associations are active
}

// BluTrvAssociation contains information about active BluTrv associations
type BluTrvAssociation struct {
	Duration  int `json:"duration"`   // Duration of the current associations procedure in seconds
	StartedAt int `json:"started_at"` // Unix timestamp of the start of the associations procedure
}

// SetConfigRequest represents the request for BLE.SetConfig
type SetConfigRequest struct {
	Config Config `json:"config"`
}

// SetConfigResponse represents the response from BLE.SetConfig
type SetConfigResponse struct {
	RestartRequired bool `json:"restart_required,omitempty"` // Whether a restart is required for changes to take effect
}

// CloudRelayListResponse represents the response from BLE.CloudRelay.List
type CloudRelayListResponse struct {
	Rev   int      `json:"rev"`   // Internal revision of the list
	Addrs []string `json:"addrs"` // The list of MAC addresses for the managed devices
}

// CloudRelayListInfosRequest represents the request for BLE.CloudRelay.ListInfos
type CloudRelayListInfosRequest struct {
	Offset int `json:"offset,omitempty"` // Optional offset of the first item to return, defaults to 0
}

// CloudRelayListInfosResponse represents the response from BLE.CloudRelay.ListInfos
type CloudRelayListInfosResponse struct {
	TS      int                        `json:"ts"`      // Unix timestamp of the response
	Offset  int                        `json:"offset"`  // Offset in the list of the first item in the current page
	Count   int                        `json:"count"`   // Number of items in the current page
	Total   int                        `json:"total"`   // Total number of items in the list
	Devices map[string]CloudRelayDevice `json:"devices"` // Device information keyed by MAC address
}

// CloudRelayDevice represents information about a device managed by Cloud Relay
type CloudRelayDevice struct {
	Name     *string           `json:"name,omitempty"`  // Device name from advertisement (populated only during active scans)
	Model    int               `json:"model,omitempty"` // Internal model id (0 = invalid, populated only during active scans)
	SData    map[string]string `json:"sdata,omitempty"` // Service data (UUID -> base64 encoded contents)
	MData    map[string]string `json:"mdata,omitempty"` // Manufacturer data (UUID -> base64 encoded contents)
	LastSeen int               `json:"last_seen"`       // Unix timestamp of the last received advertisement
}

// StartBluTrvAssociationsRequest represents the request for BLE.StartBluTrvAssociations
// Available only on devices that support BLUTRV devices (e.g., Shelly BLU Gateway Gen3)
type StartBluTrvAssociationsRequest struct {
	BluTrvID int `json:"blutrv_id,omitempty"` // Optional: BluTrv component instance ID, or discover new if not specified
	Duration int `json:"duration,omitempty"`  // Optional: Max discovery duration in seconds (defaults to 30)
	RSSIThr  int `json:"rssi_thr,omitempty"`  // Optional: RSSI threshold (defaults to -80)
}
