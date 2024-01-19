package shelly

import (
	"container/list"
	"devices/shelly/types"
	"log"
	"net"
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

func NewMdnsDevices() (*map[string]*Device, error) {
	var mdnsLookFor string = "_shelly._tcp"
	shellies := list.New()
	entriesCh := make(chan *mdns.ServiceEntry, 4)

	go func() {
		for entry := range entriesCh {
			if strings.Contains(entry.Name, mdnsLookFor) {
				shellies.PushBack(entry)
			}
		}
	}()

	// Start the lookup
	mdns.Lookup(mdnsLookFor, entriesCh)
	// time.Sleep(time.Second * 5)
	close(entriesCh)

	devices := map[string]*Device{}

	for si := shellies.Front(); si != nil; si = si.Next() {
		entry := si.Value.(*mdns.ServiceEntry)

		if _, exists := devices[entry.AddrV4.String()]; !exists {
			device, err := NewMdnsDevice(entry)
			if err != nil {
				log.Default().Printf("Discarding %v due to %v", entry, err)
			} else {
				log.Default().Printf("Loading %v: %v", entry.Name, entry)
				devices[entry.AddrV4.String()] = device
			}
		}
	}

	return &devices, nil
}

func NewMdnsDevice(entry *mdns.ServiceEntry) (*Device, error) {
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

	macStr := hostRe.ReplaceAllString(entry.Host, "${mac}")
	mac, err := net.ParseMAC(macStr)
	if err != nil {
		log.Default().Printf("Unable to parse MAC address '%v'", macStr)
	}
	var device Device = Device{
		Service:    entry.Name,
		Host:       entry.Host,
		Ipv4:       entry.AddrV4,
		MacAddress: mac,
		Port:       entry.Port,
		Product: Product{
			Model:       hostRe.ReplaceAllString(entry.Host, "${model}"),
			Generation:  generation,
			Application: application,
			Version:     version,
		},
		Methods: make(map[string]map[string]types.MethodConfiguration),
	}

	return getDeviceInfo(&device)
}
