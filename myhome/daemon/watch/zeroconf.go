package watch

import (
	"context"
	"fmt"
	"myhome"
	"myhome/devices"
	"mynet"
	"net"
	"pkg/shelly"
	"time"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

func ZeroConf(ctx context.Context, restartAfter time.Duration, dm devices.Manager, db devices.DeviceRegistry, dr mynet.Resolver) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic("BUG: No logger initialized")
	}

	go func(log logr.Logger) error {
		stopped := make(chan struct{}, 1)
		scan := make(chan *zeroconf.ServiceEntry, 1)
		for {
			err := dr.BrowseService(ctx, shelly.MDNS_SHELLIES, "local.", scan)
			if err != nil {
				log.Error(err, "Failed to start ZeroConf browser, will retry after delay")
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(restartAfter):
					continue
				}
			}
			log.Info("(Re)Started ZeroConf browser")

			go func(log logr.Logger, scan <-chan *zeroconf.ServiceEntry, stopped chan<- struct{}) error {
				for {
					select {
					case <-ctx.Done():
						// Don't log context cancellation as an error
						stopped <- struct{}{}
						return ctx.Err()

					case entry, ok := <-scan:
						if !ok {
							log.Info("ZeroConf scan channel closed, will restart browser")
							stopped <- struct{}{}
							return nil
						}
						if entry == nil {
							log.V(1).Info("Received nil entry, skipping")
							continue
						}
						log.Info("Browsed", "entry", entry)

						entry, err = completeEntry(ctx, log, dr, entry)
						if err != nil {
							log.Error(err, "Failed to complete zeroconf entry", "entry", entry)
							continue
						}

						device, err := db.GetDeviceByAny(ctx, entry.Instance)
						if err != nil || device.Info == nil {
							sd, err := shelly.NewDeviceFromZeroConfEntry(ctx, log, dr, entry)
							if err != nil {
								log.Error(err, "Failed to create device from zeroconf entry", "entry", entry)
								continue
							}
							device, err = myhome.NewDeviceFromImpl(ctx, log, sd)
							if err != nil {
								log.Error(err, "Failed to create device from shelly device", "entry", entry)
								continue
							}
						} else {
							log.Info("Found device in DB", "device_id", device.Id(), "name", device.Name())
							if device.Impl() == nil {
								log.Info("Loading device details in memory", "device_id", device.Id(), "name", device.Name())
								sd, err := shelly.NewDeviceFromSummary(ctx, log, device)
								if err != nil {
									log.Error(err, "Failed to create device from summary", "device", device)
									continue
								}
								device = device.WithImpl(sd)
							}
							device = device.WithZeroConfEntry(ctx, entry)
						}
						dm.UpdateChannel() <- device
					}
				}
			}(log.WithName("DeviceManager#WatchZeroConf"), scan, stopped)

			select {
			case <-ctx.Done():
				// Don't log context cancellation as an error
				return ctx.Err()
			case <-stopped:
				log.Info("Restarting ZeroConf browser after delay")
				time.Sleep(restartAfter)
			}
		}
	}(log.WithName("watch.ZeroConf"))

	return nil
}

func completeEntry(ctx context.Context, log logr.Logger, resolver mynet.Resolver, entry *zeroconf.ServiceEntry) (*zeroconf.ServiceEntry, error) {

	ips := make([]net.IP, 0)
	if len(entry.AddrIPv4) != 0 || len(entry.AddrIPv6) != 0 {
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

	var err error
	if len(ips) == 0 {
		log.Info("No IP in mDNS entry: resolving", "hostname", entry.HostName)
		ips, err = resolver.LookupHost(ctx, entry.HostName)
		if err != nil || len(ips) == 0 {
			log.Error(err, "Failed to resolve", "hostname", entry.HostName)
			return nil, err
		}
		if len(ips) > 0 {
			entry.AddrIPv4 = make([]net.IP, 0)
			entry.AddrIPv6 = make([]net.IP, 0)
			for _, ip := range ips {
				if ip.To4() != nil {
					entry.AddrIPv4 = append(entry.AddrIPv4, ip)
				} else {
					entry.AddrIPv6 = append(entry.AddrIPv6, ip)
				}
			}
			log.Info("Resolved from mDNS entry", "entry", entry, "ipv4", entry.AddrIPv4, "ipv6", entry.AddrIPv6)
		}
	}

	if len(ips) == 0 {
		err = fmt.Errorf("no IP addresses found for hostname %s", entry.HostName)
		return nil, err
	}

	log.V(1).Info("Resolved from mDNS entry", "entry", entry, "ipv4", entry.AddrIPv4, "ipv6", entry.AddrIPv6)
	return entry, nil
}
