package sfr

import "devices"

func Init() {
	devices.Register(ListDevices)
}
