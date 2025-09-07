package wifi

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/WiFi>

// Status contains information about the current WiFi connection state.
type Status struct {
	SSID     string `json:"ssid"`     // SSID of the network (null if disconnected)
	IP       string `json:"ip"`       // IP address of the device in the network (null if disconnected)
	Strength int    `json:"strength"` // Signal strength in dBm
}

// Network represents a scanned WiFi network.
type Network struct {
	SSID     string `json:"ssid"`     // SSID of the network
	Strength int    `json:"strength"` // Signal strength in dBm
}

// ConnectParams contains parameters for connecting to a WiFi network.
type ConnectParams struct {
	SSID     string `json:"ssid"`     // SSID of the network
	Password string `json:"password"` // Password for the network
}

type RangeExtender struct {
	Enable bool `json:"enable,omitempty"` // Set to true to enable the range extender mode
}

// AP contains information about the device's access point configuration.
type AP struct {
	Enable        bool           `json:"enable,omitempty"`         // Set to true to enable the access point configuration
	SSID          string         `json:"ssid"`                     // SSID of the access point (up to 29 symbols). Default is device ID. Set to null to restore default.
	Password      *string        `json:"pass,omitempty"`           // Password for the AP network. Default is open network. Set to null to restore default.
	IsOpen        bool           `json:"is_open,omitempty"`        // Set to true to use open network (password is ignored)
	RangeExtender *RangeExtender `json:"range_extender,omitempty"` // Set to true to enable range extender mode
}

// STA contains information about the station (client) configuration.
type STA struct {
	Enable   bool    `json:"enable,omitempty"`  // Set to true to enable the station (client) configuration
	SSID     string  `json:"ssid"`              // SSID of the network
	Password *string `json:"pass,omitempty"`    // Password for the SSID
	IsOpen   bool    `json:"is_open,omitempty"` // Set to true to use open network (password is ignored)
	IP       *string `json:"ip,omitempty"`      // IP to use when IPv4 mode is static
	Netmask  *string `json:"netmask,omitempty"` // Netmask to use when IPv4 mode is static
	Gateway  *string `json:"gateway,omitempty"` // Gateway to use when IPv4 mode is static
}

// RoamConfig represents the roaming configuration for WiFi
// See: https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/WiFi#roam
// Example fields: rssi_thr, interval
type RoamConfig struct {
	RSSIThreshold int `json:"rssi_thr"` // RSSI threshold for roaming (e.g., -80)
	Interval      int `json:"interval"` // Interval in seconds to check for roaming
}

// Config represents the WiFi configuration for the device.
type Config struct {
	AP   *AP         `json:"ap,omitempty"`   // Access point configuration
	STA  *STA        `json:"sta,omitempty"`  // Station configuration
	STA1 *STA        `json:"sta1,omitempty"` // Fallback station configuration
	Roam *RoamConfig `json:"roam"`           // Roaming configuration
}

type SetConfigRequest struct {
	Config Config `json:"config"`
}

type SetConfigResponse struct {
	Result struct {
		RestartRequired bool `json:"restart_required"`
	} `json:"result"`
}

// ScanResult represents the result of a WiFi.Scan operation (list of found networks)
type ScanResult struct {
	Results []WiFiNetwork `json:"results"`
}

// WiFiNetwork describes a single WiFi network found during scan
type WiFiNetwork struct {
	SSID    string `json:"ssid"`    // Network SSID
	BSSID   string `json:"bssid"`   // MAC address of the AP
	Auth    int    `json:"auth"`    // Auth mode (0=open, 3=WPA2, etc.)
	Channel int    `json:"channel"` // WiFi channel
	RSSI    int    `json:"rssi"`    // Signal strength (dBm)
}

// APClient represents a single WiFi client connected to the device's AP
// See: https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/WiFi#listapclients
// Example fields: mac, ip, ip_static, mport, since
type APClient struct {
	MAC      string `json:"mac"`            // MAC address of the client
	Id       string `json:"id,omitempty"`   // ID of the client
	Name     string `json:"name,omitempty"` // Name of the client
	IP       string `json:"ip"`             // IP address assigned to the client
	IPStatic bool   `json:"ip_static"`      // Whether the IP is static
	MPort    int    `json:"mport"`          // mDNS port (usually 0)
	Since    int64  `json:"since"`          // Unix timestamp when client connected
}

// ListAPClientsResult represents the result of WiFi.ListAPClients
// Contains a timestamp and a list of AP clients
type ListAPClientsResult struct {
	TS        int64      `json:"ts"`         // Unix timestamp of the response
	APClients []APClient `json:"ap_clients"` // List of connected clients
}
