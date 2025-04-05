package myhome

const MYHOME string = "myhome"

type Manufacturer string

const (
	Shelly Manufacturer = "Shelly"
)

type Verb string

const (
	DevicesMatch      Verb = "device.match"
	DeviceLookup      Verb = "device.lookup"
	DeviceShow        Verb = "device.show"
	GroupList         Verb = "group.list"
	GroupCreate       Verb = "group.create"
	GroupDelete       Verb = "group.delete"
	GroupShow         Verb = "group.show"
	GroupAddDevice    Verb = "group.adddevice"
	GroupRemoveDevice Verb = "group.removedevice"
)
