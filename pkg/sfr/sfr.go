package sfr

import (
	"net"

	"github.com/go-logr/logr"
)

func ListDevices(log logr.Logger) ([]Host, error) {
	xmlHosts, err := GetHostsList()
	if err != nil {
		log.Error(err, "Failed to get SFR hosts list")
		return nil, err
	}

	hosts := make([]Host, len(xmlHosts))
	for i, xmlHost := range xmlHosts {
		hosts[i] = Host{
			xml: xmlHost,
		}
	}

	log.Info("router knows", "count", len(hosts))
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
	mac, err := net.ParseMAC(h.xml.Mac)
	if err != nil {
		panic("BUG: Failed to parse MAC " + h.xml.Mac)
	}
	return mac
}

func (h Host) IsOnline() bool {
	return h.xml.Status == "online"
}

func (h Host) String() string {
	return h.xml.Name + " ip:" + h.xml.Ip.String() + " mac:" + h.xml.Mac + " (" + h.xml.Status + ")"
}
