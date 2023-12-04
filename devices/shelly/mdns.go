package shelly

import (
	"container/list"
	"net"
	"regexp"
	"strings"

	"github.com/hashicorp/mdns"
	"github.com/rs/zerolog/log"
)

// type MdnsEntry struct {
// 	Name       string   `json:"name"`
// 	Host       string   `json:"host"`
// 	AddrV4     net.IP   `json:"addr_v4,omitempty"`
// 	AddrV6     net.IP   `json:"addr_v6,omitempty"`
// 	Port       int      `json:"port"`
// 	Info       string   `json:"info"`
// 	InfoFields []string `json:"info_fields"`
// 	Addr       net.IP   `json:"addr"` // @Deprecated
// }

var hostRe = regexp.MustCompile("^(?P<model>[a-zA-Z0-9]+)-(?P<serial>[A-Z0-9]+).local.$")

var generationRe = regexp.MustCompile("^gen=(?P<generation>[0-9]+)$")

var applicationRe = regexp.MustCompile("^app=(?P<application>[a-zA-Z0-9]+)$")

var versionRe = regexp.MustCompile("^ver=(?P<version>[.0-9]+)$")

func MyShellies(addr net.IP) (*map[string]*Device, error) {
	var mdnsLookFor string = "_shelly._tcp"
	shellies := list.New()
	entriesCh := make(chan *mdns.ServiceEntry, 4)

	go func() {
		if addr.Equal(net.IPv4zero) {
			for entry := range entriesCh {
				if strings.Contains(entry.Name, mdnsLookFor) {
					shellies.PushBack(entry)
				}
			}
		} else {
			for entry := range entriesCh {
				if entry.AddrV4.Equal(addr) {
					shellies.PushBack(entry)
				}
			}
		}
	}()

	// Start the lookup
	mdns.Lookup("_shelly._tcp", entriesCh)
	// time.Sleep(time.Second * 5)
	close(entriesCh)

	devices := map[string]*Device{}

	for si := shellies.Front(); si != nil; si = si.Next() {
		entry := si.Value.(*mdns.ServiceEntry)

		if _, exists := devices[entry.AddrV4.String()]; !exists {
			device, err := NewDevice(entry)
			if err != nil {
				log.Logger.Debug().Msgf("Discarding %v due to %v", entry, err)
			} else {
				log.Logger.Debug().Msgf("Loading %v: %v", entry.Name, entry)
				devices[entry.AddrV4.String()] = device
			}
		}
	}

	return &devices, nil
}
