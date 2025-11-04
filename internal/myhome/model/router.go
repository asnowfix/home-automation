package model

import (
	"context"
	"net"
)

// Host represents a device on the network with IP and MAC address
type Host interface {
	Mac() net.HardwareAddr
	Name() string
	Ip() net.IP
	IsOnline() bool
	String() string
}

// Router provides access to network device information
type Router interface {
	GetHostByMac(ctx context.Context, mac net.HardwareAddr) (Host, error)
	GetHostByIp(ctx context.Context, ip net.IP) (Host, error)
	GetHostByName(ctx context.Context, name string) (Host, error)
}
