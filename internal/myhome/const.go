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
	MqttRepeat                    Verb = "mqtt.repeat"
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
)
