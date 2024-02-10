package sfr

import (
	"devices"
	"log"

	"github.com/jackpal/gateway"
)

func Init() {
	boxIp, err := gateway.DiscoverGateway()
	if err != nil {
		log.Fatal(err)
	} else {
		log.Default().Printf("assuming the box IP is %v", boxIp)
	}

	devices.Register(ListDevices)
}
