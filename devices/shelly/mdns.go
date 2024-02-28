package shelly

import (
	"container/list"
	"devices/shelly/types"
	"log"
	"strconv"
	"strings"

	"github.com/hashicorp/mdns"
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

var mdnsShellies string = "_shelly._tcp"

func MdnsShellies(tc chan string) {
	ec := make(chan *mdns.ServiceEntry, 10)

	go func() {
		for entry := range ec {
			if strings.Contains(entry.Name, mdnsShellies) {
				for _, topic := range NewDeviceFromMdns(entry).Topics() {
					tc <- topic
				}
			}
		}
	}()

	mdns.Lookup(mdnsShellies, ec)
}

func FindDevicesFromMdns() (map[string]*Device, error) {
	shellies := list.New()
	entriesCh := make(chan *mdns.ServiceEntry, 4)

	go func() {
		for entry := range entriesCh {
			if strings.Contains(entry.Name, mdnsShellies) {
				shellies.PushBack(entry)
			}
		}
	}()

	// Start the lookup
	mdns.Lookup(mdnsShellies, entriesCh)
	close(entriesCh)

	devices := make(map[string]*Device, 10)

	for si := shellies.Front(); si != nil; si = si.Next() {
		entry := si.Value.(*mdns.ServiceEntry)

		if _, exists := devices[entry.AddrV4.String()]; !exists {
			device := NewDeviceFromMdns(entry).Init()
			log.Default().Printf("Loading %v: %v", entry.Name, entry)
			devices[entry.AddrV4.String()] = device
		}
	}

	return devices, nil
}

func NewDeviceFromMdns(entry *mdns.ServiceEntry) *Device {
	log.Default().Printf("Found host:'%v'", entry.Host)
	log.Default().Printf("Found name:'%v'", entry.Name)
	log.Default().Printf("Found ipv4:'%v'", entry.AddrV4)
	log.Default().Printf("Found ipv6:'%v'", entry.AddrV6)
	log.Default().Printf("Found port:'%v'", entry.Port)

	var generation int
	var application string
	var version string
	for i, f := range entry.InfoFields {
		log.Default().Printf("Found info_field[%v]:'%v'", i, f)
		if generationRe.Match([]byte(f)) {
			generation, _ = strconv.Atoi(generationRe.ReplaceAllString(f, "${generation}"))
		}
		if applicationRe.Match([]byte(f)) {
			application = applicationRe.ReplaceAllString(f, "${application}")
		}
		if versionRe.Match([]byte(f)) {
			version = versionRe.ReplaceAllString(f, "${version}")
		}
	}
	var device Device = Device{
		Id:      nameRe.ReplaceAllString(entry.Name, "${id}"),
		Service: entry.Name,
		Host:    entry.Host,
		Ipv4:    entry.AddrV4,
		Port:    entry.Port,
		Product: Product{
			Model:       hostRe.ReplaceAllString(entry.Host, "${model}"),
			Generation:  generation,
			Application: application,
			Version:     version,
			Serial:      hostRe.ReplaceAllString(entry.Host, "${serial}"),
		},
		Components: make(map[string]map[string]types.MethodHandler),
	}

	return &device
}
