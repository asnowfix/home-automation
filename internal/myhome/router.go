package myhome

import (
	"context"
	"net"
)

type Host interface {
	Mac() net.HardwareAddr
	Name() string
	Ip() net.IP
	IsOnline() bool
	String() string
}

type Router interface {
	ListHosts(ctx context.Context) ([]Host, error)
	GetHostByMac(ctx context.Context, mac net.HardwareAddr) (Host, error)
	// GetHostByName(ctx context.Context, name string) (Host, error)
	// GetHostByIp(ctx context.Context, ip net.IP) (Host, error)
}
