package ethernet

// Config represents the Ethernet configuration
// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Eth/#configuration>

type Config struct {
	Enable     bool   `json:"enable"`      // true if the connection is enabled, false otherwise
	ServerMode bool   `json:"server_mode"` // true if the inerface is configured to operate in server mode, false if in client mode
	Ipv4Mode   string `json:"ipv4_mode"`   // IPv4 mode. Range of values: dhcp, static. Applicable only for client mode. Can be omitted if once set
	Ip         string `json:"ip"`          // IP address. Applicable only for static mode
	Netmask    string `json:"netmask"`     // Netmask of the network for client mode static and for server mode enabled. Optional, defaults to 255.255.255.0
	Gateway    string `json:"gw"`          // Gateway of the network for client mode static and for server mode enabled. Optional, set to null to clear
	Nameserver string `json:"nameserver"`  // Nameserver to use for client mode static and for server mode enabled. Optional, set to null to clear
	DhcpStart  string `json:"dhcp_start"`  // DHCP range starting IP address for the DHCP server in server mode. Applicable only for server mode. Required if not already set
	DhcpEnd    string `json:"dhcp_end"`    // DHCP range ending IP address for the DHCP server in server mode. Applicable only for server mode. Required if not already set
}

// Status represents the Ethernet status
// From <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Eth/#status>
type Status struct {
	Ip string `json:"ip"` // IP of the device in the network
}

// ListClientsResponse represents the response of the ListClients method
// From <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Eth/#listclients>
type ListClientsResponse struct {
	Ts          int64        `json:"ts"`
	Offset      int          `json:"offset"`
	Count       int          `json:"count"`
	Total       int          `json:"total"`
	DhcpClients []DhcpClient `json:"dhcp_clients"`
}

type DhcpClient struct {
	Host string `json:"host"` // Hostname of the client
	Mac  string `json:"mac"`  // MAC address of the client
	Ip   string `json:"ip"`   // IP address of the client
	Ttl  int    `json:"ttl"`  // Time to live of the client
}
