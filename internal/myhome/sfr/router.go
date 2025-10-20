package sfr

import (
	"context"
	"fmt"
	"myhome"
	"net"
	pkgsfr "pkg/sfr"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

type Router struct {
	hosts sync.Map
}

// Refreshes the IP address of the known devices every minute
func StartRouter(ctx context.Context) myhome.Router {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic(" BUG: No logger initialized")
	}

	r := &Router{}

	log.Info("Starting connected devices refresh loop")

	go func(ctx context.Context) {
		log, err = logr.FromContext(ctx)
		if err != nil {
			panic("BUG: No logger initialized")
		}
		log.Info("Started connected devices refresh loop")

		r.refresh(log)

		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Info("Exiting...")
				return
			case <-ticker.C:
				err := r.refresh(log)
				if err != nil {
					log.Error(err, "Failed to refresh devices connected to the home gateway")
				}
			}
		}
	}(logr.NewContext(ctx, log.WithName("SfrRouter")))
	return r
}

func (r *Router) refresh(log logr.Logger) error {
	devices, err := pkgsfr.ListDevices(log)
	if err != nil {
		return err
	}

	// Remove hosts that are not in the devices list
	r.hosts.Range(func(key, value any) bool {
		found := false
		for _, device := range devices {
			if device.Mac().String() == key {
				found = true
				break
			}
		}
		if !found {
			r.hosts.Delete(key)
		}
		return true
	})

	// Add listed devices
	for _, device := range devices {
		r.hosts.Store(device.Mac().String(), device)
	}

	// Count the number of stored devices
	var count int
	r.hosts.Range(func(key, value any) bool {
		count++
		return true
	})
	log.Info("Number of devices stored", "count", count)
	return nil
}

func (r *Router) GetHostByMac(ctx context.Context, mac net.HardwareAddr) (myhome.Host, error) {
	out, ok := r.hosts.Load(mac.String())
	if !ok {
		return nil, fmt.Errorf("device with MAC %s not found", mac)
	}
	host, ok := out.(myhome.Host)
	if !ok {
		return nil, fmt.Errorf("device with MAC %s is not a host %v", mac, out)
	}
	return host, nil
}
