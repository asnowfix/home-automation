package wifi

type Status struct {
	SSID     string `json:"ssid"`
	IP       string `json:"ip"`
	Strength int    `json:"strength"`
}

type Network struct {
	SSID     string `json:"ssid"`
	Strength int    `json:"strength"`
}

type ConnectParams struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

type AP struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

type STA struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
	IP       string `json:"ip,omitempty"`
	Netmask  string `json:"netmask,omitempty"`
	Gateway  string `json:"gateway,omitempty"`
}

type Configuration struct {
	Mode     string `json:"mode"`
	SSID     string `json:"ssid"`
	Password string `json:"password"`
	IP       string `json:"ip,omitempty"`
	Netmask  string `json:"netmask,omitempty"`
	Gateway  string `json:"gateway,omitempty"`
	AP       AP     `json:"ap,omitempty"`
	STA      STA    `json:"sta"`
	STA1     STA    `json:"sta1,omitempty"`
}
