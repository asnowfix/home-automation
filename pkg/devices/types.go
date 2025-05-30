package devices

import (
	"net"
)

type Output uint32

const (
	Plug Output = iota
	Heater
	Light
)

type Switch interface {
	Set() error
	Unset() error
	Status() (bool, error)
}

type Button interface {
	Action() error
	Status() (bool, error)
}

type Provider interface {
	Name() string
	Search(names []string) []Host
}

// type Devices struct {
// 	List []Device
// }

type Device interface {
	Id() string            // Device immutable Id (usually set by manufacturer)
	Name() string          // Device user-set (mutable) Name
	Host() string          // Device host address (resolvable hostname or IP address)
	Ip() net.IP            // Device IP address
	Mac() net.HardwareAddr // Device MAC address
}

type Host interface {
	Device
	Provider() string
	Online() bool
}
