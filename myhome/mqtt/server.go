package mqtt

import (
	"log"
	"mymqtt"
	"mynet"
	"net"
	"net/url"
	"os"

	"github.com/grandcat/zeroconf"

	mmqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func MyHome(program string, info []string) (*zeroconf.Server, *url.URL, error) {

	log.Default().Printf("Starting new MQTT server on %v", mymqtt.Broker(true))

	// Create the new MQTT Server.
	mqttServer := mmqtt.New(nil)

	// Allow all connections.
	_ = mqttServer.AddHook(new(auth.AllowHook), nil)

	// Create a new MQTT TCP listener on a standard port.
	tcp := listeners.NewTCP(listeners.Config{
		ID:      "tcp",
		Address: mymqtt.Broker(true).Host,
	})

	err := mqttServer.AddListener(tcp)
	if err != nil {
		log.Default().Printf("error adding TCP listener: %v", err)
		return nil, nil, err
	}

	host, err := os.Hostname()
	if err != nil {
		log.Default().Printf("error finding hostname: %v", err)
		return nil, nil, err
	}

	var instance string = program
	if program == "" {
		instance = host
	}

	// Register the service with mDNS.
	iface, _, err := mynet.MainInterface()
	ifaces := make([]net.Interface, 1)
	ifaces[0] = *iface
	if err != nil {
		log.Default().Printf("Unable to get main local IP interface: %v", err)
		return nil, nil, err
	}

	mdnsServer, err := zeroconf.Register(instance, mymqtt.ZEROCONF_SERVICE, "local.", mymqtt.PRIVATE_PORT, info, ifaces)
	if err != nil {
		log.Default().Printf("Registering new ZeroConf service: %v", err)
		return nil, nil, err
	}

	log.Default().Printf("Started new MQTT server %v ZeroConf as service: %v", mdnsServer, mymqtt.ZEROCONF_SERVICE)

	return mdnsServer, mymqtt.Broker(true), nil
}
