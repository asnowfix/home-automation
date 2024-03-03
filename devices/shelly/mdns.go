package shelly

import (
	"container/list"
	"context"
	"devices/shelly/types"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

var mdnsShellies string = "_shelly._tcp"

func MdnsShellies(tc chan string) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Fatalln("Failed to initialize resolver:", err.Error())
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			log.Println(entry)
			if strings.Contains(entry.Instance, mdnsShellies) {
				for _, topic := range NewDeviceFromMdns(entry).Topics() {
					tc <- topic
				}
			}
		}
		log.Println("No more entries.")
	}(entries)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()
	err = resolver.Browse(ctx, mdnsShellies, "local.", entries)
	if err != nil {
		log.Fatalln("Failed to browse:", err.Error())
	}

	<-ctx.Done()

}

func FindDevicesFromMdns() (map[string]*Device, error) {
	log.Default().Println("FindDevicesFromMdns()")

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Default().Fatalln("Failed to initialize resolver:", err.Error())
		return nil, err
	}

	shellies := list.New()
	entries := make(chan *zeroconf.ServiceEntry)

	go func() {
		for entry := range entries {
			log.Default().Printf("FindDevicesFromMdns(): %v", entry)

			if strings.Contains(entry.Service, mdnsShellies) {
				shellies.PushBack(entry)
			}
		}
	}()

	// Start the lookup
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	err = resolver.Browse(ctx, mdnsShellies, "local.", entries)
	if err != nil {
		log.Default().Fatalln("Failed to browse:", err.Error())
		return nil, err
	}

	<-ctx.Done()

	devices := make(map[string]*Device)

	for si := shellies.Front(); si != nil; si = si.Next() {
		entry := si.Value.(*zeroconf.ServiceEntry)

		if _, exists := devices[string(entry.AddrIPv4[0])]; !exists {
			device := NewDeviceFromMdns(entry).Init()
			log.Default().Printf("Loading %v: %v", entry.HostName, entry)
			devices[string(entry.AddrIPv4[0])] = device
		} else {
			log.Default().Printf("Dropping already known %v: %v", entry.HostName, entry)
		}
	}

	return devices, nil
}

func NewDeviceFromMdns(entry *zeroconf.ServiceEntry) *Device {
	s, _ := json.Marshal(entry)
	log.Default().Printf("Found %v", s)

	var generation int
	var application string
	var version string
	for i, txt := range entry.Text {
		log.Default().Printf("Found TXT[%v]:'%v'", i, txt)
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
	var device Device = Device{
		Id:      nameRe.ReplaceAllString(entry.HostName, "${id}"),
		Service: entry.Service,
		Host:    entry.HostName,
		Ipv4:    entry.AddrIPv4[0],
		Port:    entry.Port,
		Product: Product{
			Model:       hostRe.ReplaceAllString(entry.HostName, "${model}"),
			Generation:  generation,
			Application: application,
			Version:     version,
			Serial:      hostRe.ReplaceAllString(entry.HostName, "${serial}"),
		},
		Components: make(map[string]map[string]types.MethodHandler),
	}

	return &device
}
