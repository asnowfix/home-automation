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
				}
			}
		}
	}
	return nil, nil, fmt.Errorf("did not find any interface on the same network as the network gateway IP %v", gw)
}

var myip net.IP

func IsSameNetwork(log logr.Logger, ip net.IP) error {
	var err error
	var network *net.IPNet

	if myip == nil {
		myip, err = gateway.DiscoverInterface()
		if err != nil {
			return err
		}
	}
	log.Info("my ip", "addr", myip.String())

	maskLen, _ := myip.DefaultMask().Size()
	_, network, err = net.ParseCIDR(myip.String() + "/" + fmt.Sprintf("%d", maskLen))
	if err != nil {
		return err
	}
	log.Info("my network", "addr", network.String())

	if network.Contains(ip) {
		log.Info("ip is in my network", "ip", ip.String())
		return nil
	} else {
		log.Info("ip is not in my network", "ip", ip.String())
		return fmt.Errorf("ip is not in my network")
	}
}
