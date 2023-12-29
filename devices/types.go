package devices

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
