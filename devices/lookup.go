package devices

import (
	"fmt"
	"log"
)

func Lookup(name string) (Host, error) {
	hosts, err := List()
	if err != nil {
		log.Default().Print(err)
		return nil, err
	}
	for _, host := range hosts {
		if host.Name() == name || host.Ip().String() == name {
			return host, nil
		}
	}
	return nil, fmt.Errorf("did not find Host for name='%v'", name)
}
