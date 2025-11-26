package shelly

import (
	"context"
	"encoding/json"
	"net"
	"pkg/devices"
	"pkg/shelly/shelly"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

const MDNS_SHELLIES string = "_shelly._tcp."

func NewDeviceFromZeroConfEntry(ctx context.Context, log logr.Logger, resolver devices.Resolver, entry *zeroconf.ServiceEntry) (devices.Device, error) {
	s, _ := json.Marshal(entry)
	log.V(1).Info("Found", "entry", s)

	// deviceId is the ZeroConf instance name, e.g. "shelly1minig3-54320464074c" if matching deviceIdRe
	deviceId := ""
	if deviceIdRe.MatchString(entry.Instance) {
		deviceId = strings.ToLower(entry.Instance)
	}

	var generation int
	var application string
	var version string

	for _, txt := range entry.Text {
		log.V(1).Info("Found", "TXT", txt)
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

	d := &Device{
		Id_:   deviceId,
		Name_: entry.Instance,
		info: &shelly.DeviceInfo{
			Id: deviceId,
			Product: shelly.Product{
				Model:       hostRe.ReplaceAllString(entry.HostName, "${model}"),
				Generation:  generation,
				Application: application,
				Version:     version,
				Serial:      hostRe.ReplaceAllString(entry.HostName, "${serial}"),
			},
		},
	}

	var ip net.IP
	for _, ip = range entry.AddrIPv4 {
		if ip.IsGlobalUnicast() {
			d.UpdateHost(ip.String())
			break
		}
	}
	d.UpdateName(entry.Instance)

	log.V(1).Info("Zeroconf-discovered", "device", d)
	return d, nil
}
