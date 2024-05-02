package mqtt

import (
	"devices"
	"fmt"
	"log"
	"mynet"
	"net"
	"net/url"
	"os"
	"sync"

	"github.com/grandcat/zeroconf"
	mqtt "github.com/mochi-mqtt/server/v2"

	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

var mqttPrivatePort uint = 1883

var mqttPublicPort uint = 8883

var _privateBroker *url.URL

var _privateBrokerLock sync.Mutex

func PrivateBroker() *url.URL {
	_privateBrokerLock.Lock()
	defer _privateBrokerLock.Unlock()

	if _privateBroker == nil {
		_, ip, _ := mynet.MainInterface()
		_privateBroker = &url.URL{
			Scheme: "tcp",
			Host:   fmt.Sprintf("%s:%d", ip, mqttPrivatePort),
		}
	}
	return _privateBroker
}

func MyHome(program string, info []string) (*zeroconf.Server, *url.URL, error) {

	iface, ip, _ := mynet.MainInterface()
	mqttPrivateBroker := url.URL{
		Scheme: "tcp",
		Host:   fmt.Sprintf("%s:%d", ip, mqttPrivatePort),
	}
	log.Default().Printf("Starting new MQTT server on %v", mqttPrivateBroker)

	// Create the new MQTT Server.
	mqttServer := mqtt.New(nil)

	// Allow all connections.
	_ = mqttServer.AddHook(new(auth.AllowHook), nil)

	// Create a TCP listener on a standard port.
	tcp := listeners.NewTCP(program, mqttPrivateBroker.Host, nil)
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
	ifaces := make([]net.Interface, 1)
	ifaces[0] = *iface
	mdnsServer, err := zeroconf.Register(instance, devices.MqttService, "local.", int(mqttPrivatePort), info, ifaces)
	if err != nil {
		log.Default().Printf("Registering new ZeroConf service: %v", err)
		return nil, nil, err
	}

	log.Default().Printf("Started new MQTT server %v ZeroConf as service: %v", mdnsServer, devices.MqttService)

	return mdnsServer, &mqttPrivateBroker, nil
}
