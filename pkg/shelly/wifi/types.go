package wifi

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/WiFi

type Configuration struct {
	AP  AccessPoint `json:"ap,omitempty"`
	STA Station     `json:"sta,omitempty"`
}

type AccessPoint struct {
	SSID          string `json:"ssid"`    // SSID of the network
	Password      string `json:"pass"`    // Password for the SSID, writeonly. Must be provided if you provide ssid
	IsOpen        bool   `json:"is_open"` // True if the network is open, i.e. no password is set, false otherwise, readonly
	Enable        bool   `json:"enable"`  // True if the configuration is enabled, false otherwise
	RangeExtender struct {
		Enable bool `json:"enable"` // True if range extender functionality is enabled, false otherwise
	} `json:"range_extender"` // Range extender configuration object, available only when range extender functionality is present
}

type Station struct {
	SSID       string  `json:"ssid"`                 // SSID of the network
	Password   string  `json:"pass"`                 // Password for the SSID, writeonly. Must be provided if you provide ssid
	IsOpen     bool    `json:"is_open"`              // True if the network is open, i.e. no password is set, false otherwise, readonly
	Enable     bool    `json:"enable"`               // True if the configuration is enabled, false otherwise
	Ipv4Mode   string  `json:"ipv4_mode"`            // IPv4 mode. Range of values: dhcp, static
	Ip         *string `json:"ip,omitempty"`         // Ip to use when ipv4mode is static
	NetMask    *string `json:"netmask,omitempty"`    // Netmask to use when ipv4mode is static
	Nameserver *string `json:"nameserver,omitempty"` // Nameserver to use when ipv4mode is static
	Gateway    *string `json:"gw,omitempty"`         // Gateway to use when ipv4mode is static
}

type StatusEvent struct {
	StaIp  string `json:"sta_ip"`
	Status string `json:"status"`
	Ssid   string `json:"ssid"`
	Rssi   int    `json:"rssi"`
}

type Status struct {
	Enable    bool   `json:"enable"`
	SSID      string `json:"ssid"`
	Password  string `json:"password"`
	Mode      string `json:"mode"`
	Channel   int    `json:"channel"`
	Security  string `json:"security"`
	IPAddress string `json:"ip"`
}

type ScanStatus struct {
	Results []struct {
		SSID    *string `json:"ssid"`
		BSSID   string  `json:"bssid"`
		RSSI    int     `json:"rssi"`
		Channel int     `json:"channel"`
		Auth    uint    `json:"auth"`
	} `json:"results"`
}
