package mynet

import (
	"fmt"
	"log"
	"net"

	"github.com/jackpal/gateway"
)

func Interfaces() ([]net.Interface, error) {

	gw, err := gateway.DiscoverGateway()
	if err != nil {
		log.Default().Printf("finding network gateway: %v", err)
		return nil, err
	}
	log.Default().Printf("net gw addr: %v", gw.String())

	iface := make([]net.Interface, 1)

	ifaces, err := net.Interfaces()
	if err != nil {
		log.Default().Printf("listing interfaces: %v", err)
		return nil, err
	}
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			log.Default().Printf("finding adresses for interface %v: %v", i, err)
			continue
		}
		for _, a := range addrs {
			log.Default().Printf("%v %v", i.Name, a)
			_, nw, err := net.ParseCIDR(a.String())
			if err != nil {
				log.Default().Printf("reqding CIDR notation for %v: %v", a.String(), err)
			} else {
				if nw.Contains(gw) {
					log.Default().Printf("selecting iface %v: contains gw ip %v", a, gw)
					iface[0] = i
					return iface, nil
				} else {
					log.Default().Printf("skipping iface %v: does not contains gw ip %v", a, gw)
				}
			}
		}
	}
	return nil, fmt.Errorf("did not find any interface on the same network as the network gateway IP %v", gw)
}
