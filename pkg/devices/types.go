package devices

import (
	"encoding/json"
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
	// MarshalJSON() ([]byte, error)
}

func MarshalJSON(d Device) ([]byte, error) {
	type MarshallableDevice struct {
		Id   string           `json:"id"`
		Name string           `json:"name"`
		Host string           `json:"host"`
		Ip   net.IP           `json:"ip"`
		Mac  net.HardwareAddr `json:"mac"`
	}
	var md = MarshallableDevice{
		Name: d.Name(),
		Host: d.Host(),
		Ip:   d.Ip(),
		Id:   d.Id(),
		Mac:  d.Mac(),
	}
	return json.Marshal(md)
}

type Host interface {
	Device
	Provider() string
	Online() bool
}
