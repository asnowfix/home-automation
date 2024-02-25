package devices

import (
	"fmt"
	"log"
	"net"
)

type ListDevicesFunc func() ([]Host, error)

var listDevicesFuncs []ListDevicesFunc

func Register(f ListDevicesFunc) {
	log.Default().Print("Registering")
	listDevicesFuncs = append(listDevicesFuncs, f)
}

func List() ([]Host, error) {
	var err error
	var all []Host = make([]Host, 0)
	for _, ld := range listDevicesFuncs {
		h, err := ld()
		if err == nil {
			// log.Default().Printf("%v found matching hosts:", ld.Name(), h.Name())
			all = append(all, h...)
		} else {
			log.Default().Print(err)
		}
	}
	log.Default().Printf("found %v matching hosts:", len(all))
	return all, err
}

func Filter[T any](s []T, cond func(t T) bool) []T {
	res := []T{}
	for _, v := range s {
		if cond(v) {
			res = append(res, v)
		}
	}
	return res
}

func Topics(dn []string) ([]Topic, error) {
	hosts, err := Hosts(dn)
	if err != nil {
		log.Default().Print(err)
		return nil, err
	}
	topics := make([]Topic, len(hosts))
	log.Default().Printf("found %v hosts", len(hosts))

	for i, host := range hosts {
		topics[i] = host.Topic()
	}
	return topics, nil
}

func Hosts(args []string) ([]Host, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		log.Default().Print("not host provided: using all of them")
		return List()
	}

	hosts, err := List()
	if err != nil {
		log.Default().Print(err)
		return nil, err
	}

	ip := net.ParseIP(args[0])
	if ip == nil {
		return nil, fmt.Errorf("did not find a known Host for '%v'", args[0])
	}

	return Filter[Host](hosts, func(h Host) bool {
		if h.Name() == args[0] {
			return true
		}
		return ip.Equal(h.Ip())
	}), nil
}
