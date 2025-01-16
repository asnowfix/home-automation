package sfr

import (
	"github.com/go-logr/logr"
	"github.com/jackpal/gateway"
)

var log logr.Logger

func Init(l logr.Logger) {
	log = l
	boxIp, err := gateway.DiscoverGateway()
	if err != nil {
		log.Error(err, "failed to discover gateway")
	} else {
		log.Info("found gateway", "ip", boxIp)
	}
}
