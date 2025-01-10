package system

import "net"

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Sys/#configuration

type Configuration struct {
	Device struct {
		Name         string           `json:"name"`
		EcoMode      bool             `json:"eco_mode"`
		MacAddress   net.HardwareAddr `json:"mac"`
		FirmwareId   string           `json:"fw_id"`
		Profile      string           `json:"profile"`
		Discoverable bool             `json:"discoverable"`
		AddOnType    string           `json:"addon_type,omitempty"`
	} `json:"device"`
	Location struct {
		TimeZone  string `json:"tz,omitempty"`
		Latitude  string `json:"lat,omitempty"`
		Longitude string `json:"lon,omitempty"`
	} `json:"location"`
	Debug struct {
		Mqtt struct {
			Enable bool `json:"enable"`
		} `json:"mqtt"`
		WebSocket struct {
			Enable bool `json:"enable"`
		} `json:"websocket"`
		Udp struct {
			Enable bool `json:"enable"`
		} `json:"udp"`
	} `json:"debug"`
	RpcUdp struct {
		DestinationAddress net.IPAddr `json:"dst_addr"`
		ListenPort         uint16     `json:"listen_port,omitempty"`
	} `json:"rpc_udp"`
	Sntp struct {
		Server string `json:"server"`
	} `json:"sntp"`
	ConfigurationRevision uint32 `json:"cfg_rev"`
}

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Sys/#status

type Status struct {
	MacAddress net.HardwareAddr `json:"mac"`
	// TBC
}
