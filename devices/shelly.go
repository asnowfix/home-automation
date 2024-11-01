package devices

import (
	"devices/shelly"
	"log"
	"net"
)

type ShellyDevice struct {
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
	log.Default().Printf("Fake topic (%v) discarding '%v'.", d.Provider(), string(msg)) // TODO connect to real MQTT
}

func (d ShellyDevice) Subscribe(handler func(msg []byte)) {
	log.Default().Printf("Fake topic (%v) will not receive anything.", d.Provider()) // TODO connect to real MQTT
}

func (d ShellyDevice) MarshalJSON() ([]byte, error) {
	return MarshalJSON(d)
}

func ListShellyDevices() ([]Host, error) {
	devices, err := shelly.DevicesE()
	if err != nil {
		log.Default().Print(err)
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
		hosts[i] = sd[i]
	}
	return hosts, nil
}
