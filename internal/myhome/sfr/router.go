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

		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Info("Exiting...")
				return
			case <-ticker.C:
				devices, err := pkgsfr.ListDevices(log)
				if err != nil {
					log.Error(err, "Failed to list devices connected to the home gateway")
					continue
				}

				log.Info("Listed devices connected to the home gateway", "count", len(devices))
				for _, device := range devices {
					r.hosts.Store(device.Mac().String(), device)
				}
			}
		}
	}(logr.NewContext(ctx, log.WithName("SfrRouter")))
	return r
}

func (r *Router) ListHosts(ctx context.Context) ([]myhome.Host, error) {
	var hosts []myhome.Host
	r.hosts.Range(func(key, value any) bool {
		hosts = append(hosts, value.(myhome.Host))
		return true
	})
	return hosts, nil
}

func (r *Router) GetHostByMac(ctx context.Context, mac net.HardwareAddr) (myhome.Host, error) {
	out, ok := r.hosts.Load(mac.String())
	if !ok {
		return nil, fmt.Errorf("device with MAC %s not found", mac)
	}
	host, ok := out.(myhome.Host)
	if !ok {
		return nil, fmt.Errorf("device with MAC %s is not a host IP %v", mac, out)
	}
	return host, nil
}
