package devices

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"pkg/shelly/mqtt"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

func init() {
	log.WithName(reflect.TypeOf(empty{}).PkgPath())
}

type DeviceIdentifier struct {
	// The manufacturer of the device
	Manufacturer string `db:"manufacturer" json:"manufacturer"`
	// The unique identifier of the device, defined by the manufacturer
	ID string `db:"id" json:"id"`
}

type Device struct {
	DeviceIdentifier
	MAC    net.HardwareAddr `db:"mac" json:"mac,omitempty"` // The Ethernet hardware address of the device, globally unique & assigned by the manufacturer
	Host   string           `db:"host" json:"host"`         // The host address of the device (Host address or resolvable hostname), assigned on this network
	Name   string           `db:"name" json:"name"`         // The local unique name of the device, defined by the user
	Info   string           `db:"info"`
	Config string           `db:"config"`
	Status string           `db:"status"`

	impl any `json:"-"` // Reference to the inner implementation
}

func NewDevice(manufacturer, id string) *Device {
	return &Device{
		DeviceIdentifier: DeviceIdentifier{
			Manufacturer: manufacturer,
			ID:           id,
		},
	}
}

func (d *Device) WithMAC(mac net.HardwareAddr) *Device {
	d.MAC = mac
	return d
}

func (d *Device) WithHost(host string) *Device {
	d.Host = host
	return d
}

func (d *Device) WithName(name string) *Device {
	d.Name = name
	return d
}

func (d *Device) WithInfo(info string) *Device {
	d.Info = info
	return d
}

func (d *Device) WithConfig(config string) *Device {
	d.Config = config
	return d
}

func (d *Device) WithStatus(status string) *Device {
	d.Status = status
	return d
}

func (d *Device) WithImpl(impl any) *Device {
	d.impl = impl
	return d
}

// func (d *Device) String() string {
// 	if len(d.Name) > 0 {
// 		return d.Name
// 	}
// 	return d.ID
// }

func (d *Device) UpdateFromMqttEvent(event *mqtt.Event) error {
	// Events like:
	// - '{"src":"shelly1minig3-54320464a1d0","dst":"shelly1minig3-54320464a1d0/events","method":"NotifyStatus","params":{"ts":1736603810.49,"switch:0":{"id":0,"output":false,"source":"HTTP_in"}}}'
	// - '{"src":"shellyplus1-08b61fd90730","dst":"shellyplus1-08b61fd90730/events","method":"NotifyStatus","params":{"ts":1736604020.06,"cloud":{"connected":true}}}'
	// - '{"src":"shelly1minig3-54320464a1d0","dst":"shelly1minig3-54320464a1d0/events","method":"NotifyStatus","params":{"ts":1736605194.11,"sys":{"cfg_rev":35}}}'
	if event.Method == "NotifyStatus" {
		var status map[string]interface{}
		err := json.Unmarshal([]byte(d.Status), &status)
		if err != nil {
			log.Error(err, "failed to JSON-unmarshal status from storage: restarting with empty one", "old", d.Status)
			status = make(map[string]interface{})
		}
		if event.Params != nil {
			maps.Copy(status, *event.Params)
			out, err := json.Marshal(status)
			if err != nil {
				log.Error(err, "failed to JSON-marshal updated status")
				return err
			}
			d.Status = string(out)
		}
	}

	// - '{"src":"shellyplus1-08b61fd141e8","dst":"shellyplus1-08b61fd141e8/events","method":"NotifyFullStatus","params":{"ts":1736604018.38,"ble":{},"cloud":{"connected":true},"input:0":{"id":0,"state":false},"mqtt":{"connected":true},"switch:0":{"id":0, "source":"SHC", "output":true,"temperature":{"tC":48.4, "tF":119.2}},"sys":{"mac":"08B61FD141E8","restart_required":false,"time":"15:00","unixtime":1736604018,"uptime":658773,"ram_size":268520,"ram_free":110248,"fs_size":393216,"fs_free":106496,"cfg_rev":13,"kvs_rev":0,"schedule_rev":1,"webhook_rev":0,"available_updates":{"beta":{"version":"1.5.0-beta1"}},"reset_reason":3},"wifi":{"sta_ip":"192.168.1.76","status":"got ip","ssid":"Linksys_7A50","rssi":-58,"ap_client_count":0},"ws":{"connected":false}}}'
	if event.Method == "NotifyFullStatus" {
		out, err := json.Marshal(event.Params)
		if err != nil {
			log.Error(err, "failed to marshal updated full status")
			return err
		}
		d.Status = string(out)
	}

	// - '{"dst":"NCELRND1279_shellyplus1-08b61fd9333c","error":{"code":-109,"message":"shutting down in 952 ms"},"id":0,"result":{"methods":null},"src":"shellyplus1-08b61fd9333c"}'
	// - '{"src":"shelly1minig3-54320464a1d0","dst":"shelly1minig3-54320464a1d0/events","method":"NotifyEvent","params":{"ts":1736605194.11,"events":[{"component":"input:0","id":0,"event":"config_changed","restart_required":false,"ts":1736605194.11,"cfg_rev":35}]}}'
	if event.Method == "NotifyEvent" {
		if event.Params != nil {
			evs, ok := (*event.Params)["events"].([]mqtt.ComponentEvent)
			if ok {
				log.Info("Received event", "events", evs)
			} else {
				return fmt.Errorf("unable to parse event parameters: %v", event)
			}
		} else {
			return fmt.Errorf("missing event parameters in event: %v", event)
		}
	}

	return nil
}
