package mynet

import (
	"fmt"
	"log"
	"net"

	"github.com/jackpal/gateway"
)

func MainInterface() (*net.Interface, *net.IP, error) {

	gw, err := gateway.DiscoverGateway()
	if err != nil {
		log.Default().Printf("finding network gateway: %v", err)
		return nil, nil, err
	}
	log.Default().Printf("net gw addr: %v", gw.String())

	ifaces, err := net.Interfaces()
	if err != nil {
		log.Default().Printf("listing interfaces: %v", err)
		return nil, nil, err
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			log.Default().Printf("finding adresses for interface %v: %v", iface, err)
			continue
		}
		for _, addr := range addrs {
			log.Default().Printf("%v %v", iface.Name, addr)
			ip, nw, err := net.ParseCIDR(addr.String())
			if err != nil {
				log.Default().Printf("reading CIDR notation for %v: %v", addr.String(), err)
			} else {
				if nw.Contains(gw) {
					log.Default().Printf("selecting iface %v with ip %v: contains gw ip %v", addr, ip, gw)

					return &iface, &ip, nil
				} else {
					log.Default().Printf("skipping iface %v: does not contains gw ip %v", addr, gw)
				}
			}
		}
	}
	return nil, nil, fmt.Errorf("did not find any interface on the same network as the network gateway IP %v", gw)
}
