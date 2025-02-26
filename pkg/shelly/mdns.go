package shelly

import (
	"encoding/json"
	"net"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

const MDNS_SHELLIES string = "_shelly._tcp."

func NewDeviceFromZeroConfEntry(log logr.Logger, entry *zeroconf.ServiceEntry) (*Device, error) {
	s, _ := json.Marshal(entry)
	log.Info("Found", "entry", s)

	var generation int
	var application string
	var version string
	for _, txt := range entry.Text {
		log.Info("Found", "TXT", txt)
		if generationRe.Match([]byte(txt)) {
			generation, _ = strconv.Atoi(generationRe.ReplaceAllString(txt, "${generation}"))
		}
		if applicationRe.Match([]byte(txt)) {
			application = applicationRe.ReplaceAllString(txt, "${application}")
		}
		if versionRe.Match([]byte(txt)) {
			version = versionRe.ReplaceAllString(txt, "${version}")
		}
	}

	ips, err := net.LookupIP(entry.HostName)
	if err != nil {
		log.Error(err, "Failed to resolve IP address", "hostname", entry.HostName)
		return nil, err
	}

	var ip net.IP
	if len(ips) > 0 {
		log.Info("Resolved", "hostname", entry.HostName, "ip[]", ips)
		for _, ip = range ips {
			if ip.To4() != nil {
				break
			}
		}
	} else {
		log.Error(nil, "No IP addresses found for hostname", "hostname", entry.HostName)
		return nil, err
	}

	d := &Device{
		Id_:     entry.Instance,
		Service: entry.Service,
		Host_:   ip.String(),
		Port:    entry.Port,
		Product: Product{
			Model:       hostRe.ReplaceAllString(entry.HostName, "${model}"),
			Generation:  generation,
			Application: application,
			Version:     version,
			Serial:      hostRe.ReplaceAllString(entry.HostName, "${serial}"),
		},
	}
	log.Info("Discovered", "device", d)

	return d, nil
}
