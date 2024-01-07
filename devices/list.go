package devices

import "log"

type ListDevicesFunc func() ([]Host, error)

var listDevicesFuncs []ListDevicesFunc

func Register(f ListDevicesFunc) {
	listDevicesFuncs = append(listDevicesFuncs, f)
}

func List() ([]Host, error) {
	var err error
	var all []Host = make([]Host, 1)
	for _, ld := range listDevicesFuncs {
		h, err := ld()
		if err == nil {
			all = append(all, h...)
		} else {
			log.Default().Print(err)
		}
	}
	return all, err
}
