package types

type MethodParams map[string]string

type MethodConfiguration struct {
	Allocate func() any
	Params   MethodParams
}

type ConfigurationMethod func(m string, c MethodConfiguration)

type Api uint

const (
	Shelly Api = iota
	Schedule
	Webhook
	HTTP
	KVS
	System
	WiFi
	Ethernet
	BluetoothLowEnergy
	Cloud
	MQTT
	OutboundWebsocket
	Script
	Input
	Modbus
	Voltmeter
	Cover
	Switch
	Light
	DevicePower
	Humidity
	Temperature
	None
)

func (api Api) String() string {
	return [...]string{
		"Shelly",
		"Schedule",
		"Webhook",
		"HTTP",
		"KVS",
		"System",
		"WiFi",
		"Ethernet",
		"BluetoothLowEnergy",
		"Cloud",
		"MQTT",
		"OutboundWebsocket",
		"Script",
		"Input",
		"Modbus",
		"Voltmeter",
		"Cover",
		"Switch",
		"Light",
		"DevicePower",
		"Humidity",
		"Temperature",
		"None",
	}[api]
}
