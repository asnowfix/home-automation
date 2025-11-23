package shelly

import (
	"encoding/json"
	"pkg/shelly/ethernet"
	"pkg/shelly/mqtt"
	"pkg/shelly/sswitch"
	"pkg/shelly/system"
	"pkg/shelly/wifi"
	"schedule"
)

type Product struct {
	Model       string `json:"model"`
	Serial      string `json:"serial,omitempty"`
	MacAddress  string `json:"mac"`
	Application string `json:"app"`
	Version     string `json:"ver"`
	Generation  int    `json:"gen"`
}

type State uint32

type DeviceInfo struct {
	Product
	Name                  *string     `json:"name,omitempty"`
	Id                    string      `json:"id"`
	FirmwareId            string      `json:"fw_id"`
	Profile               string      `json:"profile,omitempty"`
	AuthenticationEnabled bool        `json:"auth_en"`
	AuthenticationDomain  string      `json:"auth_domain,omitempty"`
	Discoverable          bool        `json:"discoverable,omitempty"`
	CloudKey              string      `json:"key,omitempty"`
	Batch                 string      `json:"batch,omitempty"`
	FirmwareSBits         string      `json:"fw_sbits,omitempty"`
	Slot                  int         `json:"slot,omitempty"` // Pro2,  PlugSG3, not documented
	Matter                bool        `json:"matter,omitempty"`
	BTHome                *BTHomeInfo `json:"bthome,omitempty"` // BTHome BLE Protocol information (for BLU devices)
}

// BTHomeInfo contains BTHome BLE protocol information for BLU devices
type BTHomeInfo struct {
	Version          int               `json:"version"`                     // BTHome protocol version (e.g., 2)
	Encryption       bool              `json:"encryption"`                  // Whether device uses encryption
	Capabilities     []string          `json:"capabilities,omitempty"`      // Sensor capabilities (temperature, humidity, motion, etc.)
	ServiceData      map[string]string `json:"service_data,omitempty"`      // Raw service data (UUID: hex string)
	ManufacturerData map[string]string `json:"manufacturer_data,omitempty"` // Raw manufacturer data (if present)
}

type Config struct {
	BLE       *any                 `json:"ble,omitempty"`
	BtHome    *any                 `json:"bthome,omitempty"`
	Cloud     *any                 `json:"cloud,omitempty"`
	Ethernet  *ethernet.Config     `json:"eth,omitempty"`
	Input0    *sswitch.InputConfig `json:"input:0,omitempty"`
	Input1    *sswitch.InputConfig `json:"input:1,omitempty"`
	Input2    *sswitch.InputConfig `json:"input:2,omitempty"`
	Input3    *sswitch.InputConfig `json:"input:3,omitempty"`
	Knx       *any                 `json:"knx,omitempty"`
	Mqtt      *mqtt.Config         `json:"mqtt,omitempty"`
	Schedule  *schedule.Scheduled  `json:"schedule,omitempty"`
	Switch0   *sswitch.Config      `json:"switch:0,omitempty"`
	Switch1   *sswitch.Config      `json:"switch:1,omitempty"`
	Switch2   *sswitch.Config      `json:"switch:2,omitempty"`
	Switch3   *sswitch.Config      `json:"switch:3,omitempty"`
	System    *system.Config       `json:"system,omitempty"`
	Wifi      *wifi.Config         `json:"wifi,omitempty"`
	WebSocket *any                 `json:"ws,omitempty"`
}

type Status struct {
	// gen1 only
	Gen1 *map[string]float32 `json:"gen1,omitempty"`

	// gen2+
	BLE       *any                 `json:"ble,omitempty"`
	BtHome    *any                 `json:"bthome,omitempty"`
	Cloud     *any                 `json:"cloud,omitempty"`
	Ethernet  *ethernet.Status     `json:"eth,omitempty"`
	Input0    *sswitch.InputStatus `json:"input:0,omitempty"`
	Input1    *sswitch.InputStatus `json:"input:1,omitempty"`
	Input2    *sswitch.InputStatus `json:"input:2,omitempty"`
	Input3    *sswitch.InputStatus `json:"input:3,omitempty"`
	Knx       *any                 `json:"knx,omitempty"`
	Mqtt      *mqtt.Status         `json:"mqtt,omitempty"`
	Switch0   *sswitch.Status      `json:"switch:0,omitempty"`
	Switch1   *sswitch.Status      `json:"switch:1,omitempty"`
	Switch2   *sswitch.Status      `json:"switch:2,omitempty"`
	Switch3   *sswitch.Status      `json:"switch:3,omitempty"`
	System    *system.Status       `json:"system,omitempty"`
	Wifi      *wifi.Status         `json:"wifi,omitempty"`
	WebSocket *any                 `json:"ws,omitempty"`
}

// From https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Shelly#shellycheckforupdate

type MethodsResponse struct {
	Methods []string `json:"methods"`
}

type CheckForUpdateResponse struct {
	Stable *struct {
		Version string `json:"version"`  // The version of the stable firmware
		BuildId string `json:"build_id"` // The build ID of the stable firmware
	} `json:"stable,omitempty"`
	Beta *struct {
		Version string `json:"version"`  // The version of the beta firmware
		BuildId string `json:"build_id"` // The build ID of the beta firmware
	} `json:"beta,omitempty"`
}

// From https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Shelly#shellygetcomponents
type ComponentsRequest struct {
	Offset      int      `json:"offset,omitempty"`       // Index of the component from which to start generating the result Optional
	Include     []string `json:"include,omitempty"`      // "status" will include the component's status, "config" - the config. The keys are always included. Combination of both (["config", "status"]) to get the full config and status of each component. Optional
	Keys        []string `json:"keys,omitempty"`         // An array of component keys in the format <type> <cid> (for example, boolean:200) which is used to filter the response list. If empty/not provided, all components will be returned. Optional
	DynamicOnly bool     `json:"dynamic_only,omitempty"` // If true, only dynamic components will be returned. Optional
}

type Components struct {
	Config *Config `json:"-"`
	Status *Status `json:"-"`
}

type ComponentsResponse struct {
	Components
	Response_      *[]ComponentResponse `json:"components"`
	ConfigRevision int                  `json:"cfg_revision"` // The current config revision. See SystemGetConfig#ConfigRevision
	Offset         int                  `json:"offset"`       // The index of the first component in the list.
	Total          int                  `json:"total"`        // Total number of components with all filters applied.
}

func (cr *ComponentsResponse) UnmarshalJSON(data []byte) error {
	type Alias ComponentsResponse
	if err := json.Unmarshal(data, (*Alias)(cr)); err != nil {
		return err
	}
	if cr.Response_ == nil {
		cr.Response_ = &[]ComponentResponse{}
	}
	config := make(map[string]any)
	status := make(map[string]any)

	for _, comp := range *cr.Response_ {
		config[comp.Key] = comp.Config
		status[comp.Key] = comp.Status
	}

	configStr, err := json.Marshal(config)
	if err != nil {
		return err
	}
	cr.Config = &Config{}
	if err := json.Unmarshal(configStr, cr.Config); err != nil {
		return err
	}
	statusStr, err := json.Marshal(status)
	if err != nil {
		return err
	}
	cr.Status = &Status{}
	if err := json.Unmarshal(statusStr, cr.Status); err != nil {
		return err
	}
	return nil
}

type ComponentResponse struct {
	Key    string         `json:"key"`    // Component's key (in format <type>:<cid>, for example boolean:200)
	Config map[string]any `json:"config"` // Component's config, will be omitted if "config" is not specified in the include property.
	Status map[string]any `json:"status"` // Component's status, will be omitted if "status" is not specified in the include property.
	// Methods map[string]types.MethodHandler `json:"-"`
}
