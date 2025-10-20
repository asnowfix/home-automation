package sfr

import (
	"net"

	"github.com/go-logr/logr"
)

func ListDevices(log logr.Logger) ([]Host, error) {
	xmlHosts, err := LanGetHostsList()
	if err != nil {
		log.Error(err, "Failed to get SFR hosts list")
		return nil, err
	}

	hosts := make([]Host, len(xmlHosts))
	for i, xmlHost := range xmlHosts {
		log.Info("Found SFR host", "hostname", xmlHost.Name)
		hosts[i] = Host{
			xml: xmlHost,
		}
	}

	return hosts, nil
}

type Host struct {
	xml *XmlHost
}

func (h Host) Name() string {
	return h.xml.Name
}

func (h Host) Ip() net.IP {
	return h.xml.Ip
}

func (h Host) Mac() net.HardwareAddr {
	return h.xml.Mac
}

func (h Host) Online() bool {
	return h.xml.Status == "online"
}
