package devices

import (
	"devices/sfr"
	"devices/shelly"
)

func Init() {
	shelly.Init()
	Register(ListShellyDevices)

	sfr.Init()
	Register(ListSfrDevices)
}
