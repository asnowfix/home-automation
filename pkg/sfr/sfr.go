package sfr

import (
	"net"
	"pkg/devices"

	"github.com/go-logr/logr"
)

func ListSfrDevices(log logr.Logger) ([]devices.Host, error) {
	xmlHosts, err := LanGetHostsList()
	if err != nil {
		log.Error(err, "Failed to get SFR hosts list")
		return nil, err
	}

	hosts := make([]devices.Host, len(xmlHosts))
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
	xml *XmlHost
}

func (h SfrHost) Provider() string {
	return "sfrbox"
}

func (h SfrHost) Name() string {
	return h.xml.Name
}

func (h SfrHost) Id() string {
	return h.xml.Mac.String()
}

func (h SfrHost) Ip() net.IP {
	return h.xml.Ip
}

func (h SfrHost) Host() string {
	return h.xml.Ip.String()
}

func (h SfrHost) Online() bool {
	return h.xml.Status == "online"
}

func (h SfrHost) IsConnected() bool {
	return false
}

func (h SfrHost) MarshalJSON() ([]byte, error) {
	return devices.MarshalJSON(h)
}
