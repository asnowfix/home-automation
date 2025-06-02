package shelly

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"pkg/devices"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

const MDNS_SHELLIES string = "_shelly._tcp."

func NewDeviceFromZeroConfEntry(ctx context.Context, log logr.Logger, resolver devices.Resolver, entry *zeroconf.ServiceEntry) (*Device, error) {
	s, _ := json.Marshal(entry)
	log.Info("Found", "entry", s)

	// deviceId is the ZeroConf instance name, e.g. "shelly1minig3-54320464074c" if matching deviceIdRe
	deviceId := ""
	isMqttOk := false
	if deviceIdRe.MatchString(entry.Instance) {
		deviceId = strings.ToLower(entry.Instance)
		isMqttOk = true
	}

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

	var err error
	var ips []net.IP
	if len(entry.AddrIPv4) != 0 || len(entry.AddrIPv6) != 0 {
		ips = make([]net.IP, 0, len(entry.AddrIPv4)+len(entry.AddrIPv6))
		for _, ip := range entry.AddrIPv4 {
			if !ip.IsLinkLocalUnicast() {
				ips = append(ips, ip)
			}
		}
		for _, ip := range entry.AddrIPv6 {
			if !ip.IsLinkLocalUnicast() {
				ips = append(ips, ip)
			}
		}
	}

	if len(ips) == 0 {
		ips, err = resolver.LookupHost(ctx, entry.HostName)
		if err != nil {
			log.Error(err, "Failed to resolve IP address", "hostname", entry.HostName)
			return nil, err
		}
	}

	if len(ips) > 0 {
		log.Info("Resolved", "hostname", entry.HostName, "ip[]", ips)
	} else {
		err = fmt.Errorf("no IP addresses found for hostname %s", entry.HostName)
		return nil, err
	}

	d := &Device{
		isMqttOk: isMqttOk,
		Id_:      deviceId,
		Service:  entry.Service,
		Host_:    ips[0].String(),
		Port:     entry.Port,
		Product: Product{
			Model:       hostRe.ReplaceAllString(entry.HostName, "${model}"),
			Generation:  generation,
			Application: application,
			Version:     version,
			Serial:      hostRe.ReplaceAllString(entry.HostName, "${serial}"),
		},
	}
	log.Info("Zeroconf discovered", "device", d)
	return d, nil
}
