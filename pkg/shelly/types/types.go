package types

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"
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

	Channel(ctx context.Context, via Channel) Channel

	UpdateName(name string)
	UpdateHost(host string)
	ClearHost()
	UpdateMac(mac string)
	UpdateId(id string)
	IsModified() bool
	ResetModified()
}

type DeviceCaller func(ctx context.Context, device Device, mh MethodHandler, out any, params any) (any, error)

// HostResolver resolves a device's current dialable IP address, e.g. via
// mDNS or a router's ARP-like lookup table. Implemented by the internal/
// myhome layer, which owns network-topology concerns, and injected here at
// daemon startup via SetHostResolver — keeping pkg/shelly free of any
// MyHome-specific dependency (see CLAUDE.md's Three-Tier Layer Rule).
type HostResolver interface {
	ResolveHost(ctx context.Context, mac net.HardwareAddr, name string) (net.IP, error)
}

var hostResolver HostResolver

// SetHostResolver installs the HostResolver used to (re-)resolve a device's
// IP address when it is unknown, or immediately after an HTTP dial failure,
// before falling back to another channel (e.g. MQTT). Call once at daemon
// startup; nil (the default) disables resolution.
func SetHostResolver(r HostResolver) {
	hostResolver = r
}

// ResolveHost calls the installed HostResolver, if any. ok is false if no
// resolver is installed, or the installed one could not find an address.
func ResolveHost(ctx context.Context, mac net.HardwareAddr, name string) (ip net.IP, ok bool) {
	if hostResolver == nil {
		return nil, false
	}
	log := logr.FromContextOrDiscard(ctx)
	start := time.Now()
	ip, err := hostResolver.ResolveHost(ctx, mac, name)
	log.Info("ResolveHost", "mac", mac, "name", name, "ip", ip, "err", err, "elapsed", time.Since(start))
	if err != nil || ip == nil {
		return nil, false
	}
	return ip, true
}

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

// apiNames maps each Api constant to its display name. Unlike a positional
// array literal, this can't silently desynchronize when a new Api is
// inserted in the const block above: every entry is keyed by the constant
// itself, so it tracks wherever that constant's iota value ends up. See
// TestApiString_AllConstantsNamed, which fails loudly if a constant is added
// here without a matching entry.
var apiNames = map[Api]string{
	Shelly:             "Shelly",
	Schedule:           "Schedule",
	Webhook:            "Webhook",
	HTTP:               "HTTP",
	KVS:                "KVS",
	System:             "System",
	WiFi:               "WiFi",
	Ethernet:           "Ethernet",
	BluetoothLowEnergy: "BluetoothLowEnergy",
	Cloud:              "Cloud",
	Mqtt:               "Mqtt",
	OutboundWebsocket:  "OutboundWebsocket",
	Script:             "Script",
	Input:              "Input",
	Modbus:             "Modbus",
	Voltmeter:          "Voltmeter",
	Cover:              "Cover",
	Switch:             "Switch",
	Light:              "Light",
	DevicePower:        "DevicePower",
	Humidity:           "Humidity",
	Temperature:        "Temperature",
	None:               "None",
}

func (api Api) String() string {
	if name, ok := apiNames[api]; ok {
		return name
	}
	return fmt.Sprintf("Api(%d)", uint32(api))
}
