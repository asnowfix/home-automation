package mqtt

import (
	"devices"
	"fmt"
	"log"
	"os"

	"github.com/grandcat/zeroconf"
	mqtt "github.com/mochi-mqtt/server/v2"

	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

var mqttPrivatePort uint = 1883

func MyHome(program string, info []string) (*zeroconf.Server, error) {
	// Create the new MQTT Server.
	mqttServer := mqtt.New(nil)

	// Allow all connections.
	_ = mqttServer.AddHook(new(auth.AllowHook), nil)

	// ip, err := gateway.DiscoverInterface()
	// if err != nil {
	// 	log.Default().Printf("error finding IP to reach network gateway: %v", err)
	// 	return nil, err
	// }
	// addr := fmt.Sprintf("%v:%v", ip, mqttPrivatePort)
	addr := fmt.Sprintf(":%v", mqttPrivatePort)

	// Create a TCP listener on a standard port.
	tcp := listeners.NewTCP(program, addr, nil)
	err := mqttServer.AddListener(tcp)
	if err != nil {
		log.Default().Printf("error adding TCP listener: %v", err)
		return nil, err
	}

	host, err := os.Hostname()
	if err != nil {
		log.Default().Printf("error finding hostname: %v", err)
		return nil, err
	}

	var instance string = program
	if program == "" {
		instance = host
	}

	// ifaces, err := mynet.Interfaces()
	// if err != nil {
	// log.Default().Printf("error finding interfaceto the gateway: %v", err)
	// return nil, err
	// }

	mdnsServer, err := zeroconf.Register(instance, devices.MqttService, "local.", 1883, info, nil /*ifaces*/)
	if err != nil {
		log.Default().Printf("registering new ZeroConf service: %v", err)
		return nil, err
	}

	log.Default().Printf("Started new MQTT server %v ZeroConf as service: %v", mdnsServer, devices.MqttService)

	return mdnsServer, nil
}
