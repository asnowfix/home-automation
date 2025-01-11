package devices

import (
	"net"
	"pkg/shelly"

	"github.com/go-logr/logr"
)

type ShellyDevice struct {
	log    logr.Logger
	shelly *shelly.Device
}

func (d ShellyDevice) Provider() string {
	return "shelly"
}

func (d ShellyDevice) Name() string {
	return d.shelly.Host
}

func (d ShellyDevice) Ip() net.IP {
	return d.shelly.Ipv4()
}

func (d ShellyDevice) Mac() net.HardwareAddr {
	return d.shelly.MacAddress
}

func (d ShellyDevice) Online() bool {
	return true // TODO because found by mDNS
}

func (d ShellyDevice) Topic() Topic {
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
	return MarshalJSON(d)
}

func ListShellyDevices(log logr.Logger) ([]Host, error) {
	devices, err := shelly.DevicesE(log)
	if err != nil {
		log.Error(err, "Unable to list Shelly devices")
		return nil, err
	}
	sd := make([]ShellyDevice, len(devices))
	hosts := make([]Host, len(devices))

	// Extract keys of a map as a slice (pre go 1.23)
	keys := make([]string, len(devices))
	i := 0
	for k := range devices {
		keys[i] = k
		i++
	}

	for i := range keys {
		sd[i].shelly = devices[keys[i]]
		sd[i].log = log
		hosts[i] = sd[i]
	}
	return hosts, nil
}
