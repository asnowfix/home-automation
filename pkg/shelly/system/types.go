package system

import "net"

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Sys>

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Sys#syssetconfig-example>
type SetConfigRequest struct {
	Config Config `json:"config"`
}

type SetConfigResponse struct {
	RestartRequired bool `json:"restart_required"`
}

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Sys/#configuration

type Config struct {
	Device   *DeviceConfig `json:"device,omitempty"`
	Location *struct {
		TimeZone  string  `json:"tz,omitempty"`
		Latitude  float32 `json:"lat,omitempty"`
		Longitude float32 `json:"lon,omitempty"`
	} `json:"location,omitempty"`
	Debug  *DeviceDebug `json:"debug,omitempty"`
	RpcUdp *struct {
		DestinationAddress string `json:"dst_addr"`
		ListenPort         uint16 `json:"listen_port,omitempty"`
	} `json:"rpc_udp,omitempty"`
	Sntp *struct {
		Server string `json:"server"`
	} `json:"sntp,omitempty"`
	ConfigRevision uint32 `json:"cfg_rev,omitempty"`
}

type DeviceConfig struct {
	Name         string           `json:"name,omitempty"`
	EcoMode      bool             `json:"eco_mode,omitempty"`
	MacAddress   net.HardwareAddr `json:"mac,omitempty"`
	FirmwareId   string           `json:"fw_id,omitempty"`
	Profile      string           `json:"profile,omitempty"`
	Discoverable bool             `json:"discoverable,omitempty"`
	AddOnType    string           `json:"addon_type,omitempty"`
}

type Enabler struct {
	Enable bool `json:"enable"`
	Level  int  `json:"level,omitempty"`
}

type EnablerUDP struct {
	Address *string `json:"addr"`
	Level   int     `json:"level,omitempty"`
}

type DeviceDebug struct {
	Mqtt      Enabler    `json:"mqtt"`
	WebSocket Enabler    `json:"websocket"`
	Udp       EnablerUDP `json:"udp"`
}

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Sys/#status

type Status struct {
	MacAddress       net.HardwareAddr `json:"mac"`
	RestartRequired  bool             `json:"restart_required"`
	CurrentTime      string           `json:"time"`
	UnixTime         uint32           `json:"unixtime"`
	UpTime           uint32           `json:"uptime"`
	RamSize          uint32           `json:"ram_size"`
	RamFree          uint32           `json:"ram_free"`
	FsSize           uint32           `json:"fs_size"`
	FsFree           uint32           `json:"fs_free"`
	ConfigRevision   uint32           `json:"cfg_rev"`
	KvsRevision      uint32           `json:"kvs_rev"`
	ScheduleRevision uint32           `json:"schedule_rev"`
	WebHookRevision  uint32           `json:"webhook_rev"`
	AvailableUpdates struct {
		Beta *struct {
			Version string `json:"version"`
			Url     string `json:"url"`
		} `json:"beta,omitempty"`
		Stable *struct {
			Version string `json:"version"`
			Url     string `json:"url"`
		} `json:"stable,omitempty"`
	} `json:"available_updates"`
	ResetReason uint32 `json:"reset_reason"`
}
