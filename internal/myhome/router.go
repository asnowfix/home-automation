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
	GetHostByMac(ctx context.Context, mac net.HardwareAddr) (Host, error)
}
