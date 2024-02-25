package mqtt

import (
	"devices"
	"log"
	"net"
	"os"

	"github.com/jackpal/gateway"
	mqtt "github.com/mochi-mqtt/server/v2"

	"github.com/hashicorp/mdns"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func MyHome(program string, info []string) (*mdns.Server, error) {
	// Create the new MQTT Server.
	mqttServer := mqtt.New(nil)

	// Allow all connections.
	_ = mqttServer.AddHook(new(auth.AllowHook), nil)

	// Create a TCP listener on a standard port.
	tcp := listeners.NewTCP("t1", ":1883", nil)
	err := mqttServer.AddListener(tcp)
	if err != nil {
		log.Fatal(err)
	}

	addrs := make([]net.IP, 1)
	addrs[0], err = gateway.DiscoverInterface()
	if err != nil {
		log.Fatal(err)
	}

	host, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}

	var instance string = program
	if program == "" {
		instance = host
	}

	log.Default().Printf("publishing %v as %v over mDNS via IP %v", info, devices.MqttService, addrs)
	mdnsService, _ := mdns.NewMDNSService(instance, devices.MqttService, "" /*domain*/, "" /*host*/, 1883, addrs, info)

	// Create the mDNS server, defer shutdown
	mdnsServer, _ := mdns.NewServer(&mdns.Config{Zone: mdnsService})

	go func() {
		err := mqttServer.Serve()
		if err != nil {
			log.Fatal(err)
		}
	}()

	return mdnsServer, nil
}
