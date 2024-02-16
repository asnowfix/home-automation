package mqtt

import (
	"devices"
	"log"
	"os"

	mqtt "github.com/mochi-mqtt/server/v2"

	"github.com/hashicorp/mdns"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func MyHome(info []string) (*mdns.Server, error) {
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

	host, _ := os.Hostname()
	mdnsService, _ := mdns.NewMDNSService(host, devices.MqttService, "", "", 1883, nil, info)
	log.Default().Printf("publishing %v as %v over mDNS", info, devices.MqttService)

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
