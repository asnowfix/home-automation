package shelly

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/asnowfix/home-automation/pkg/shelly/shelly"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

const MDNS_SHELLIES string = "_shelly._tcp."

// Resolver is the narrow lookup surface NewDeviceFromZeroConfEntry accepts —
// declared locally so pkg/shelly does not depend on the app's pkg/devices
// package (see CLAUDE.md's Three-Tier Layer Rule). Any type satisfying these
// two methods, including pkg/devices.Resolver, already implements it.
type Resolver interface {
	LookupHost(ctx context.Context, log logr.Logger, host string) (ips []net.IP, err error)
	LookupService(ctx context.Context, service string) (*url.URL, error)
}

func NewDeviceFromZeroConfEntry(ctx context.Context, log logr.Logger, resolver Resolver, entry *zeroconf.ServiceEntry) (*Device, error) {
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
		id:   deviceId,
		name: entry.Instance,
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
