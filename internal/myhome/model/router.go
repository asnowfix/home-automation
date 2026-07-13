package model

import (
	"context"
	"errors"
	"net"
)

// ErrNotFound is returned (or wrapped) by a Router implementation when it has
// no record for the requested MAC/IP/name. Chain callers use it to decide
// whether to fall through to the next Router; see Chain and NotFoundRouter.
var ErrNotFound = errors.New("host not found")

// Host represents a device on the network with IP and MAC address
type Host interface {
	Mac() net.HardwareAddr
	Name() string
	Ip() net.IP
	IsOnline() bool
	String() string
}

// Router provides access to network device information.
//
// Implementations MUST only ever resolve to an address that is directly
// HTTP-dialable from this process (i.e. a plain routable IP on the local
// network). A device that is only reachable by relaying a call through
// another gateway device (e.g. a Shelly acting as a NAT'ing Wi-Fi access
// point, or a router like the TP-Link Omada EAP 100) is out of scope for
// this interface — see https://github.com/asnowfix/home-automation/issues/336.
type Router interface {
	GetHostByMac(ctx context.Context, mac net.HardwareAddr) (Host, error)
	GetHostByIp(ctx context.Context, ip net.IP) (Host, error)
	GetHostByName(ctx context.Context, name string) (Host, error)
}
