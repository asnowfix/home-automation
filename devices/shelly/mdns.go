package shelly

import (
	"container/list"
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

const MDNS_SHELLIES string = "_shelly._tcp"

func mdnsShellies(tc chan string) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Fatalln("Failed to initialize resolver:", err.Error())
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			log.Println(entry)
			if strings.Contains(entry.Instance, MDNS_SHELLIES) {
				for _, topic := range newDeviceFromMdns(entry).Topics() {
					tc <- topic
				}
			}
		}
		log.Println("No more entries.")
	}(entries)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()
	err = resolver.Browse(ctx, MDNS_SHELLIES, "local.", entries)
	if err != nil {
		log.Fatalln("Failed to browse:", err.Error())
	}

	<-ctx.Done()

}

func loadDevicesFromMdns() error {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Default().Fatalln("Failed to initialize zeroconf resolver:", err.Error())
		return err
	}

	shellies := list.New()
	entries := make(chan *zeroconf.ServiceEntry)

	go func() {
		for entry := range entries {
			log.Default().Printf("Found %v", entry)

			if strings.Contains(entry.Service, MDNS_SHELLIES) {
				shellies.PushBack(entry)
			}
		}
	}()

	// Start the lookup
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	err = resolver.Browse(ctx, MDNS_SHELLIES, "local.", entries)
	if err != nil {
		log.Default().Fatalln("Failed to browse:", err.Error())
		return err
	}

	<-ctx.Done()

	for si := shellies.Front(); si != nil; si = si.Next() {
		entry := si.Value.(*zeroconf.ServiceEntry)
		if len(entry.AddrIPv4) == 0 {
			log.Default().Printf("Skipping mDNS entry without an IPv4: %v", entry)
			continue
		}
		addDevice(entry.AddrIPv4[0].String(), newDeviceFromMdns(entry))
	}

	return nil
}

func newDeviceFromMdns(entry *zeroconf.ServiceEntry) *Device {
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
	d := &Device{
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
	}
	d.Init()
	return d
}
