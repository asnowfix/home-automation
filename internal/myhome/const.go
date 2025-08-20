package myhome

const MYHOME string = "myhome"

// MYHOME_HOSTNAME is the base hostname published via mDNS (e.g., "myhome.local").
const MYHOME_HOSTNAME string = "myhome"

type Manufacturer string

const (
	SHELLY Manufacturer = "Shelly"
)

type Verb string

const (
	DevicesMatch      Verb = "device.match"
	DeviceLookup      Verb = "device.lookup"
	DeviceShow        Verb = "device.show"
	DeviceForget      Verb = "device.forget"
	GroupList         Verb = "group.list"
	GroupCreate       Verb = "group.create"
	GroupDelete       Verb = "group.delete"
	GroupShow         Verb = "group.show"
	GroupAddDevice    Verb = "group.adddevice"
	GroupRemoveDevice Verb = "group.removedevice"
)
