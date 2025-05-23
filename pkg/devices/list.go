package devices

import (
	"fmt"
	"net"

	"github.com/go-logr/logr"
)

type ListDevicesFunc func() ([]Host, error)

var listDevicesFuncs []ListDevicesFunc

func Register(log logr.Logger, f ListDevicesFunc) {
	log.Info("Registering")
	listDevicesFuncs = append(listDevicesFuncs, f)
}

func List(log logr.Logger) ([]Host, error) {
	var err error
	var all []Host = make([]Host, 0)
	for _, ld := range listDevicesFuncs {
		h, err := ld()
		if err == nil {
			all = append(all, h...)
		} else {
			log.Error(err, "did not find matching host")
		}
	}
	log.Info("found matching hosts", "len", len(all))
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

func Hosts(log logr.Logger, args []string) ([]Host, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		log.Info("not host provided: using all of them")
		return List(log)
	}

	hosts, err := List(log)
	if err != nil {
		log.Info("cannot list hosts", err)
		return nil, err
	}

	ip := net.ParseIP(args[0])
	if ip == nil {
		return nil, fmt.Errorf("did not find a known Host for hostname %v", args[0])
	}

	return Filter[Host](hosts, func(h Host) bool {
		if h.Name() == args[0] {
			return true
		}
		return ip.Equal(h.Ip())
	}), nil
}
