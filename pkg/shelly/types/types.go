package types

import (
	"net"
)

type MethodsRegistrar interface {
	RegisterMethodHandler(comp string, verb string, mh MethodHandler)
	RegisterDeviceCaller(ch Channel, dc DeviceCaller)
	CallE(d Device, ch Channel, mh MethodHandler, params any) (any, error)
}

type Device interface {
	String() string
	Ipv4() net.IP
	Id() string
	CallE(via Channel, comp string, verb string, params any) (any, error)
	MqttChannel() chan []byte
}

type DeviceCaller func(device Device, mh MethodHandler, out any, params any) (any, error)

type Channel uint

const (
	ChannelHttp Channel = iota
	ChannelMqtt
	ChannelUdp
)

func (ch Channel) String() string {
	return [...]string{"Http", "Mqtt", "Udp"}[ch]
}

type MethodHandler struct {
	Method     string     `json:"method"`      // The method name
	Allocate   func() any `json:"-"`           // Allocate a new instance of the output type
	HttpMethod string     `json:"http_method"` // The HTTP request method to use (See https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods)
}

var MethodNotFound = MethodHandler{}

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
	Mqtt
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
		"Mqtt",
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
