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
		TimeZone  string  `json:"tz,omitempty"`
		Latitude  float32 `json:"lat,omitempty"`
		Longitude float32 `json:"lon,omitempty"`
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
	ConfigRevision uint32 `json:"cfg_rev"`
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
