package types

import (
	"context"
	"fmt"
	"net"
)

type MethodsRegistrar interface {
	RegisterMethodHandler(method string, mh MethodHandler)
	RegisterDeviceCaller(ch Channel, dc DeviceCaller)
	CallE(ctx context.Context, d Device, ch Channel, mh MethodHandler, params any) (any, error)
}

type Device interface {
	String() string
	Name() string
	Host() string
	Manufacturer() string
	Id() string
	Mac() net.HardwareAddr
	CallE(ctx context.Context, via Channel, method string, params any) (any, error)
	ReplyTo() string
	To() chan<- []byte
	From() <-chan []byte
	StartDialog(ctx context.Context) uint32
	StopDialog(ctx context.Context, id uint32)

	IsHttpReady() bool

	IsMqttReady() bool

	Channel(via Channel) Channel

	UpdateName(name string)
	UpdateHost(host string)
	ClearHost()
	UpdateMac(mac string)
	UpdateId(id string)
	IsModified() bool
	ResetModified()
}

type DeviceCaller func(ctx context.Context, device Device, mh MethodHandler, out any, params any) (any, error)

type Channel uint32

var Channels = [...]string{"default", "http", "mqtt", "udp"}

const (
	ChannelDefault Channel = iota
	ChannelHttp
	ChannelMqtt
	ChannelUdp
)

func (ch Channel) String() string {
	return Channels[ch]
}

func ParseChannel(s string) (Channel, error) {
	for i, ch := range Channels {
		if ch == s {
			return Channel(i), nil
		}
	}
	return ChannelDefault, fmt.Errorf("unknown channel %s (expected one of %v)", s, Channels)
}

type MethodHandler struct {
	Method     string
	Allocate   func() any
	HttpMethod string
}

var MethodNotFound = MethodHandler{}

var NotAMethod = MethodHandler{}

type Api uint32

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
