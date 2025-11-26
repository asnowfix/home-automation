package sfr

import (
	"context"
	"fmt"
	"myhome/model"
	"net"
	"pkg/sfr"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

type Router struct {
	macs  sync.Map
	ips   sync.Map
	names sync.Map
}

var router *Router

var routerLock sync.Mutex

// Refreshes the IP address of the known devices every minute
func GetRouter(ctx context.Context) model.Router {
	routerLock.Lock()
	defer routerLock.Unlock()

	if router != nil {
		return router
	}

	log, err := logr.FromContext(ctx)
	if err != nil {
		panic(" BUG: No logger initialized: " + err.Error())
	}

	r := &Router{}

	log.Info("Starting connected devices refresh loop")

	go func(ctx context.Context) {
		log, err = logr.FromContext(ctx)
		if err != nil {
			panic("BUG: No logger initialized: " + err.Error())
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
	router = r
	return r
}

func (r *Router) refresh(log logr.Logger) error {
	devices, err := sfr.ListDevices(log)
	if err != nil {
		return err
	}

	// Remove hosts that are not in the devices list
	r.macs.Range(func(key, value any) bool {
		found := false
		for _, device := range devices {
			if device.Mac().String() == key {
				found = true
				break
			}
		}
		if !found {
			r.macs.Delete(key)
			r.ips.Delete(key)
			r.names.Delete(key)
		}
		return true
	})

	// Add listed devices
	for _, device := range devices {
		r.macs.Store(device.Mac().String(), device)
		r.ips.Store(device.Ip().String(), device)
		r.names.Store(device.Name(), device)
	}

	// Count the number of stored devices
	var count int
	r.macs.Range(func(key, value any) bool {
		count++
		return true
	})
	log.V(1).Info("Number of devices stored", "count", count)

	return nil
}

func (r *Router) GetHostByMac(ctx context.Context, mac net.HardwareAddr) (model.Host, error) {
	out, ok := r.macs.Load(mac.String())
	if !ok {
		return nil, fmt.Errorf("device with MAC %s not found", mac)
	}
	host, ok := out.(model.Host)
	if !ok {
		return nil, fmt.Errorf("device with MAC %s is not a host %v", mac, out)
	}
	return host, nil
}

func (r *Router) GetHostByIp(ctx context.Context, ip net.IP) (model.Host, error) {
	out, ok := r.ips.Load(ip.String())
	if !ok {
		return nil, fmt.Errorf("device with IP %s not found", ip)
	}
	host, ok := out.(model.Host)
	if !ok {
		return nil, fmt.Errorf("device with IP %s is not a host %v", ip, out)
	}
	return host, nil
}

func (r *Router) GetHostByName(ctx context.Context, name string) (model.Host, error) {
	out, ok := r.names.Load(name)
	if !ok {
		return nil, fmt.Errorf("device with name %s not found", name)
	}
	host, ok := out.(model.Host)
	if !ok {
		return nil, fmt.Errorf("device with name %s is not a host %v", name, out)
	}
	return host, nil
}
