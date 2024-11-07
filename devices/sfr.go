package devices

import (
	"devices/sfr"
	"net"

	"github.com/go-logr/logr"
)

func ListSfrDevices(log logr.Logger) ([]Host, error) {
	xmlHosts, err := sfr.LanGetHostsList()
	if err != nil {
		log.Error(err, "Failed to get SFR hosts list")
		return nil, err
	}

	hosts := make([]Host, len(xmlHosts))
	sh := make([]SfrHost, len(xmlHosts))
	for i, xmlHost := range xmlHosts {
		log.Info("Found SFR host", "hostname", xmlHost.Name)
		sh[i] = SfrHost{
			xml: xmlHost,
			log: log,
		}
		hosts[i] = sh[i]
	}

	return hosts, nil
}

type SfrHost struct {
	log logr.Logger
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
	h.log.Info("Fake topic, discarding message.", "topic", h.Provider(), "msg", string(msg)) // TODO connect to real MQTT
}

func (h SfrHost) Subscribe(handler func(msg []byte)) {
	h.log.Info("Fake topic, will not receive anything.", "topic", h.Provider()) // TODO connect to real MQTT
}

func (h SfrHost) MarshalJSON() ([]byte, error) {
	return MarshalJSON(h)
}
