package shelly

import (
	"devices"
	"net"

	"github.com/go-logr/logr"
)

type ShellyDevice struct {
	log    logr.Logger
	shelly *Device
}

func (d ShellyDevice) Provider() string {
	return "shelly"
}

func (d ShellyDevice) Name() string {
	return d.shelly.Host()
}

func (d ShellyDevice) Mac() net.HardwareAddr {
	return d.shelly.MacAddress
}

func (d ShellyDevice) Online() bool {
	return true // TODO because found by mDNS
}

func (d ShellyDevice) Topic() devices.Topic {
	return nil // TODO connect to real MQTT
}

func (d ShellyDevice) IsConnected() bool {
	return false // TODO connect to real MQTT
}

func (d ShellyDevice) Publish(msg []byte) {
	d.log.Info("Fake topic discarding message.", "topic", d.Provider(), "msg", string(msg)) // TODO connect to real MQTT
}

func (d ShellyDevice) Subscribe(handler func(msg []byte)) {
	d.log.Info("Fake topic will not receive anything.", "topic", d.Provider()) // TODO connect to real MQTT
}

func (d ShellyDevice) MarshalJSON() ([]byte, error) {
	return devices.MarshalJSON(d)
}

func (d ShellyDevice) Ip() net.IP {
	if ip := net.ParseIP(d.shelly.Host()); ip != nil {
		return ip
	}
	// ips, err := d.shelly.LookupHost(ctx, d.shelly.Host())
	// if err != nil {
	// 	d.log.Error(err, "Failed to resolve IP of", "hostname", d.shelly.Host())
	// 	return nil
	// }
	// for _, ip := range ips {
	// 	if ip.To4() != nil {
	// 		d.shelly.SetHost(ip.String())
	// 		return ip
	// 	}
	// }
	return nil
}
