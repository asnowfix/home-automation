package shelly

import (
	"container/list"
	"context"
	"devices/shelly/types"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

const MDNS_SHELLIES string = "_shelly._tcp"

func mdnsShellies(log logr.Logger, tc chan string) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Error(err, "Failed to initialize resolver")
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			log.Info("entry: %v", entry)
			if strings.Contains(entry.Instance, MDNS_SHELLIES) {
				for _, topic := range newDeviceFromMdns(log, entry).Topics() {
					tc <- topic
				}
			}
		}
		log.Info("No more entries.")
	}(entries)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()
	err = resolver.Browse(ctx, MDNS_SHELLIES, "local.", entries)
	if err != nil {
		log.Error(err, "Failed to browse")
	}

	<-ctx.Done()

}

func loadDevicesFromMdns(log logr.Logger) error {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Error(err, "Failed to initialize zeroconf resolver")
		return err
	}

	shellies := list.New()
	entries := make(chan *zeroconf.ServiceEntry)

	go func() {
		for entry := range entries {
			log.Info("Found", " entry", entry)

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
		log.Error(err, "Failed to browse")
		return err
	}

	<-ctx.Done()

	for si := shellies.Front(); si != nil; si = si.Next() {
		entry := si.Value.(*zeroconf.ServiceEntry)
		if len(entry.AddrIPv4) == 0 {
			log.Info("Skipping mDNS entry without an IPv4: %v", entry)
			continue
		}
		addDevice(log, entry.AddrIPv4[0].String(), newDeviceFromMdns(log, entry))
	}

	return nil
}

func newDeviceFromMdns(log logr.Logger, entry *zeroconf.ServiceEntry) *Device {
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
	d := &Device{
		Id_:     nameRe.ReplaceAllString(entry.HostName, "${id}"),
		Service: entry.Service,
		Host:    entry.HostName,
		Ipv4_:   entry.AddrIPv4[0],
		Port:    entry.Port,
		Product: Product{
			Model:       hostRe.ReplaceAllString(entry.HostName, "${model}"),
			Generation:  generation,
			Application: application,
			Version:     version,
			Serial:      hostRe.ReplaceAllString(entry.HostName, "${serial}"),
		},
	}
	// Initialize device usign http channel and default channel http type
	d.Init(types.ChannelHttp)
	return d
}
