package devices

import (
	"encoding/json"
	"fmt"
	"maps"
	"myhome"
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

type Device struct {
	myhome.Device
	impl any `json:"-"` // Reference to the inner implementation
}

type Group struct {
	myhome.Group
}

func NewDevice(manufacturer, id string) *Device {
	return &Device{
		Device: myhome.Device{
			DeviceIdentifier: myhome.DeviceIdentifier{
				Manufacturer: manufacturer,
				ID:           id,
			},
		},
		impl: nil,
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

// func (d *Device) WithInfo(info string) *Device {
// 	d.Info = info
// 	return d
// }

// func (d *Device) WithConfig(config string) *Device {
// 	d.Config = config
// 	return d
// }

// func (d *Device) WithStatus(status string) *Device {
// 	d.Status = status
// 	return d
// }

func (d *Device) WithImpl(impl any) *Device {
	d.impl = impl
	return d
}

func (d *Device) UpdateFromMqttEvent(event *mqtt.Event) error {
	// Events like:
	// - '{"src":"shelly1minig3-54320464a1d0","dst":"shelly1minig3-54320464a1d0/events","method":"NotifyStatus","params":{"ts":1736603810.49,"switch:0":{"id":0,"output":false,"source":"HTTP_in"}}}'
	// - '{"src":"shellyplus1-08b61fd90730","dst":"shellyplus1-08b61fd90730/events","method":"NotifyStatus","params":{"ts":1736604020.06,"cloud":{"connected":true}}}'
	// - '{"src":"shelly1minig3-54320464a1d0","dst":"shelly1minig3-54320464a1d0/events","method":"NotifyStatus","params":{"ts":1736605194.11,"sys":{"cfg_rev":35}}}'
	if event.Method == "NotifyStatus" {
		if event.Params != nil {
			var err error
			status := make(map[string]interface{})
			if d.Status != nil {
				// FIXME: Convoluted way to merge status update map event in the current status
				out, err := json.Marshal(d.Status)
				if err != nil {
					log.Error(err, "failed to JSON-marshal current status")
					return err
				}
				err = json.Unmarshal(out, &status)
				if err != nil {
					log.Error(err, "failed to unmarshal current status")
					return err
				}
			}
			maps.Copy(status, *event.Params)
			out, err := json.Marshal(status)
			if err != nil {
				log.Error(err, "failed to JSON-(re)marshal updated status")
				return err
			}
			err = json.Unmarshal(out, &d.Status)
			if err != nil {
				log.Error(err, "failed to (re)unmarshal updated status")
				return err
			}
			// v := reflect.ValueOf(d.Status)
			// for i := 0; i < v.NumField(); i++ {
			// 	typeField := v.Type().Field(i)
			// 	valueField := v.Field(i)
			// 	log.Info("Updated status", "field", typeField.Name, "value", valueField.Interface())
			// }
		}
	}

	// - '{"src":"shellyplus1-08b61fd141e8","dst":"shellyplus1-08b61fd141e8/events","method":"NotifyFullStatus","params":{"ts":1736604018.38,"ble":{},"cloud":{"connected":true},"input:0":{"id":0,"state":false},"mqtt":{"connected":true},"switch:0":{"id":0, "source":"SHC", "output":true,"temperature":{"tC":48.4, "tF":119.2}},"sys":{"mac":"08B61FD141E8","restart_required":false,"time":"15:00","unixtime":1736604018,"uptime":658773,"ram_size":268520,"ram_free":110248,"fs_size":393216,"fs_free":106496,"cfg_rev":13,"kvs_rev":0,"schedule_rev":1,"webhook_rev":0,"available_updates":{"beta":{"version":"1.5.0-beta1"}},"reset_reason":3},"wifi":{"sta_ip":"192.168.1.76","status":"got ip","ssid":"Linksys_7A50","rssi":-58,"ap_client_count":0},"ws":{"connected":false}}}'
	if event.Method == "NotifyFullStatus" {
		out, err := json.Marshal(event.Params)
		if err != nil {
			log.Error(err, "failed to marshal updated full status")
			return err
		}
		err = json.Unmarshal(out, &d.Status)
		if err != nil {
			log.Error(err, "failed to unmarshal updated full status")
			return err
		}
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

// func NewDeviceFromShelly(sd *shelly.Device) (*Device, error) {
// 	d := NewDevice(Shelly, sd.Id())
// 	d = d.WithMAC(net.HardwareAddr(sd.Info.MacAddress.String()))
// 	d = d.WithHost(sd.Ipv4().String())
// 	d = d.WithName(sd.Config.Sys.DeviceName)
// 	info, err := json.Marshal(sd.Info)
// 	if err != nil {
// 		log.Error(err, "failed to marshal shelly info")
// 		return nil, err
// 	}
// 	d = d.WithInfo(string(info))
// 	d = d.WithConfig(sd.Config)
// 	d = d.WithStatus(sd.Status)
// 	d = d.WithGroups(sd.Groups)
// 	return d
// }
