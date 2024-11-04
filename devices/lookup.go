package devices

import (
	"fmt"

	"github.com/go-logr/logr"
)

func Lookup(log logr.Logger, name string) (*Host, error) {
	hosts, err := List(log)
	if err != nil {
		log.Info("did not find host named", name, err)
		return nil, err
	}
	for _, host := range hosts {
		if host.Name() == name || host.Ip().String() == name {
			return &host, nil
		}
	}
	return nil, fmt.Errorf("did not find Host for name='%v'", name)
}
