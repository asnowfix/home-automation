package shelly

import (
	"container/list"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"

	"github.com/hashicorp/mdns"
	"github.com/rs/zerolog/log"
)

type MdnsEntry struct {
	Name       string   `json:"name"`
	Host       string   `json:"host"`
	AddrV4     net.IP   `json:"addr_v4,omitempty"`
	AddrV6     net.IP   `json:"addr_v6,omitempty"`
	Port       int      `json:"port"`
	Info       string   `json:"info"`
	InfoFields []string `json:"info_fields"`
	Addr       net.IP   `json:"addr"` // @Deprecated
}

var hostRe = regexp.MustCompile("^(?P<model>[a-zA-Z0-9]+)-(?P<serial>[A-Z0-9]+).local.$")

var generationRe = regexp.MustCompile("^gen=(?P<generation>[0-9]+)$")

var applicationRe = regexp.MustCompile("^app=(?P<application>[a-zA-Z0-9]+)$")

var versionRe = regexp.MustCompile("^ver=(?P<version>[.0-9]+)$")

func MyShellies() (*list.List, error) {
	devices := list.New()
	entriesCh := make(chan *mdns.ServiceEntry, 4)
	go func() {
		for entry := range entriesCh {

			var pe = MdnsEntry{
				Name:       entry.Name,
				Host:       entry.Host,
				AddrV4:     entry.AddrV4,
				AddrV6:     entry.AddrV6,
				Port:       entry.Port,
				Info:       entry.Info,
				InfoFields: entry.InfoFields,
				Addr:       entry.Addr,
			}
			out, err := json.Marshal(pe)
			if err != nil {
				log.Logger.Debug().Msgf("Discarding %v due to %v", pe, err)
				return
			}

			var generation int
			var application string
			var version string
			for i, f := range entry.InfoFields {
				log.Logger.Debug().Msgf("info_field[%v] '%v'", i, f)
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

			// gen, err := strconv.Atoi(genRe.ReplaceAllString(entry.Info, "${gen}"))
			// if err != nil {
			// 	log.Logger.Debug().Msgf("Discarding %v due to %v", pe, err)
			// 	return
			// }

			var device = Device{
				Host:        entry.Host,
				Ip:          entry.AddrV4,
				Model:       hostRe.ReplaceAllString(entry.Host, "${model}"),
				Serial:      hostRe.ReplaceAllString(entry.Host, "${serial}"),
				Generation:  generation,
				Application: application,
				Version:     version,
			}

			out, err = json.Marshal(device)
			if err != nil {
				log.Logger.Debug().Msgf("Discarding %v due to %v", pe, err)
				return
			}

			if device.Host != device.Model {
				fmt.Printf("Got new shelly device: %v\n", string(out))
				devices.PushBack(device)
			} else {
				fmt.Printf("Discarding device: %v\n", string(out))
			}
		}
	}()

	// Start the lookup
	mdns.Lookup("_shelly._tcp", entriesCh)
	close(entriesCh)
	return devices, nil
}
