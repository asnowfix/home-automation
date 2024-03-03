package mqtt

import (
	"devices"
	"log"
	"mynet"
	"os"

	"github.com/grandcat/zeroconf"
	mqtt "github.com/mochi-mqtt/server/v2"

	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func MyHome(program string, info []string) (*zeroconf.Server, error) {
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

	host, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}

	var instance string = program
	if program == "" {
		instance = host
	}

	ifaces, err := mynet.Interfaces()
	if err != nil {
		log.Fatal(err)
	}

	mdnsServer, err := zeroconf.Register(instance, devices.MqttService, "local.", 1883, info, ifaces)
	if err != nil {
		log.Fatal(err)
	}

	return mdnsServer, nil
}
