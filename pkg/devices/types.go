package devices

import (
	"encoding/json"
	"net"
)

type Output uint

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

type Host interface {
	Provider() string
	Name() string
	Ip() net.IP
	Mac() net.HardwareAddr
	Online() bool
	Topic() Topic
	MarshalJSON() ([]byte, error)
}

func MarshalJSON(h Host) ([]byte, error) {
	type MarshalledHost struct {
		Provider string           `json:"provider"`
		Name     string           `json:"name"`
		Ip       net.IP           `json:"ip"`
		Mac      net.HardwareAddr `json:"mac"`
		Online   bool             `json:"online"`
	}
	var hs = MarshalledHost{
		Provider: h.Provider(),
		Name:     h.Name(),
		Ip:       h.Ip(),
		Mac:      h.Mac(),
		Online:   h.Online(),
	}
	return json.Marshal(hs)
}

type Topic interface {
	IsConnected() bool
	Publish(msg []byte)
	Subscribe(handler func(msg []byte))
}
