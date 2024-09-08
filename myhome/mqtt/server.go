package mqtt

import (
	"log"
	"mqtt"
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

	log.Default().Printf("Starting new MQTT server on %v", mqtt.Broker(true))

	// Create the new MQTT Server.
	mqttServer := mmqtt.New(nil)

	// Allow all connections.
	_ = mqttServer.AddHook(new(auth.AllowHook), nil)

	// Create a TCP listener on a standard port.
	tcp := listeners.NewTCP(program, mqtt.Broker(true).Host, nil)
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

	mdnsServer, err := zeroconf.Register(instance, mqtt.ZEROCONF_SERVICE, "local.", mqtt.PRIVATE_PORT, info, ifaces)
	if err != nil {
		log.Default().Printf("Registering new ZeroConf service: %v", err)
		return nil, nil, err
	}

	log.Default().Printf("Started new MQTT server %v ZeroConf as service: %v", mdnsServer, mqtt.ZEROCONF_SERVICE)

	return mdnsServer, mqtt.Broker(true), nil
}
