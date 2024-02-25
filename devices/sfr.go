package devices

import (
	"devices/sfr"
	"log"
	"net"
)

func ListSfrDevices() ([]Host, error) {
	xmlHosts, err := sfr.LanGetHostsList()
	if err != nil {
		log.Default().Print(err)
		return nil, err
	}

	hosts := make([]Host, len(xmlHosts))
	sh := make([]SfrHost, len(xmlHosts))
	for i, xmlHost := range xmlHosts {
		log.Default().Println(xmlHost)
		sh[i] = SfrHost{
			xml: xmlHost,
		}
		hosts[i] = sh[i]
	}

	return hosts, nil
}

type SfrHost struct {
	xml *sfr.XmlHost
}

func (h SfrHost) Provider() string {
	return "sfrbox"
}

func (h SfrHost) Name() string {
	return h.xml.Name
}

func (h SfrHost) Ip() net.IP {
	return h.xml.Ip
}

func (h SfrHost) Mac() net.HardwareAddr {
	return h.xml.Mac
}

func (h SfrHost) Online() bool {
	return h.xml.Status == "online"
}

func (h SfrHost) Topic() Topic {
	return nil
}

func (h SfrHost) IsConnected() bool {
	return false
}

func (h SfrHost) Publish(msg []byte) {
	log.Default().Printf("Fake topic (%v) discarding '%v'.", h.Provider(), string(msg)) // TODO connect to real MQTT
}

func (h SfrHost) Subscribe(handler func(msg []byte)) {
	log.Default().Printf("Fake topic (%v) will not receive anything.", h.Provider()) // TODO connect to real MQTT
}

func (h SfrHost) MarshalJSON() ([]byte, error) {
	return MarshalJSON(h)
}
