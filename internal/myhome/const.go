package myhome

const MYHOME string = "myhome"

type Verb string

const (
	DeviceList        Verb = "device.list"
	DeviceShow        Verb = "device.show"
	GroupList         Verb = "group.list"
	GroupCreate       Verb = "group.create"
	GroupDelete       Verb = "group.delete"
	GroupListDevices  Verb = "group.getdevices"
	GroupAddDevice    Verb = "group.adddevice"
	GroupRemoveDevice Verb = "group.removedevice"
)
