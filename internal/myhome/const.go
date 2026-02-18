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
	DevicesMatch                  Verb = "device.match"
	DeviceLookup                  Verb = "device.lookup"
	DeviceShow                    Verb = "device.show"
	DeviceForget                  Verb = "device.forget"
	DeviceRefresh                 Verb = "device.refresh"
	DeviceSetup                   Verb = "device.setup"
	DeviceUpdate                  Verb = "device.update"
	TemperatureGet                Verb = "temperature.get"
	TemperatureSet                Verb = "temperature.set"
	TemperatureList               Verb = "temperature.list"
	TemperatureDelete             Verb = "temperature.delete"
	TemperatureGetSchedule        Verb = "temperature.getschedule"
	TemperatureGetWeekdayDefaults Verb = "temperature.getweekdaydefaults"
	TemperatureSetWeekdayDefault  Verb = "temperature.setweekdaydefault"
	TemperatureGetKindSchedules   Verb = "temperature.getkindschedules"
	TemperatureSetKindSchedule    Verb = "temperature.setkindschedule"
	OccupancyGetStatus            Verb = "occupancy.getstatus"
	HeaterGetConfig               Verb = "heater.getconfig"
	HeaterSetConfig               Verb = "heater.setconfig"
	ThermometerList               Verb = "thermometer.list"
	DoorList                      Verb = "door.list"
	RoomList                      Verb = "room.list"
	RoomCreate                    Verb = "room.create"
	RoomEdit                      Verb = "room.edit"
	RoomDelete                    Verb = "room.delete"
	DeviceSetRoom                 Verb = "device.setroom"
	DeviceListByRoom              Verb = "device.listbyroom"
	SwitchToggle                  Verb = "switch.toggle"
	SwitchOn                      Verb = "switch.on"
	SwitchOff                     Verb = "switch.off"
	SwitchStatus                  Verb = "switch.status"
	SwitchAll                     Verb = "switch.all"
)

type Key string

const (
	NormallyClosedKey Key = "normally-closed"
	RoomIdKey         Key = "room-id"
)
