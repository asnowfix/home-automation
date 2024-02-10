package devices

import "net"

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
	Name() string
	Ip() net.IP
	Mac() net.HardwareAddr
	Online() bool
	Topic() Topic
}

type Topic interface {
	IsConnected() bool
	Publish(msg []byte)
	Subscribe(handler func(msg []byte))
}

var NoTopic Topic
