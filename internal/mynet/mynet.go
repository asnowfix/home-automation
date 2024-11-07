package mynet

import (
	"fmt"
	"net"

	"github.com/go-logr/logr"
	"github.com/jackpal/gateway"
)

func MainInterface(log logr.Logger) (*net.Interface, *net.IP, error) {

	gw, err := gateway.DiscoverGateway()
	if err != nil {
		log.Info("finding network gateway: %v", err)
		return nil, nil, err
	}
	log.Info("net gw ", "addr", gw.String())

	ifaces, err := net.Interfaces()
	if err != nil {
		log.Error(err, "listing interfaces")
		return nil, nil, err
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			log.Error(err, "finding adresses", "interface", iface)
			continue
		}
		for _, addr := range addrs {
			log.Info("", "interface", iface.Name, "addr", addr)
			ip, nw, err := net.ParseCIDR(addr.String())
			if err != nil {
				log.Error(err, "reading CIDR notation", "iface_addr", addr.String())
			} else {
				if nw.Contains(gw) {
					log.Info("selecting iface: contains gw ip", "iface_addr", addr, "iface_ip", ip, "gw_ip", gw)

					return &iface, &ip, nil
				} else {
					log.Info("skipping iface: does not contains gw ip", "iface_addr", addr, "gw_ip", gw)
				}
			}
		}
	}
	return nil, nil, fmt.Errorf("did not find any interface on the same network as the network gateway IP %v", gw)
}
