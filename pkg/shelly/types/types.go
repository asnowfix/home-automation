package types

import (
	"context"
	"net"
)

type MethodsRegistrar interface {
	RegisterMethodHandler(method any, mh MethodHandler)
	RegisterDeviceCaller(ch Channel, dc DeviceCaller)
	CallE(ctx context.Context, d Device, ch Channel, mh MethodHandler, params any) (any, error)
}

type Device interface {
	String() string
	Ipv4() net.IP
	Id() string
	CallE(ctx context.Context, via Channel, method any, params any) (any, error)
	ReplyTo() string
	To() chan<- []byte
	From() <-chan []byte
}

type DeviceCaller func(ctx context.Context, device Device, mh MethodHandler, out any, params any) (any, error)

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
	Method     string
	Allocate   func() any
	HttpMethod string
}

var MethodNotFound = MethodHandler{}

var NotAMethod = MethodHandler{}

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
