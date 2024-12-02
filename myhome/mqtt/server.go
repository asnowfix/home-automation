package mqtt

import (
	"log/slog"
	"mymqtt"
	"mynet"
	"net"
	"net/url"
	"os"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"

	mmqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/hooks/debug"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func MyHome(log logr.Logger, program string, info []string) (*zeroconf.Server, *url.URL, error) {

	log.Info("Starting new MQTT server", "broker", mymqtt.Broker(log, true))

	// Create the new MQTT Server.
	mqttServer := mmqtt.New(nil)

	// Configure the MQTT server so that every message is logged.
	mqttServer.Log = slog.New(logr.ToSlogHandler(log))
	err := mqttServer.AddHook(&debug.Hook{
		Log: slog.New(logr.ToSlogHandler(log)),
	}, &debug.Options{
		ShowPacketData: true,
		ShowPings:      true,
		ShowPasswords:  true,
	})
	if err != nil {
		log.Error(err, "error adding debug hook")
		return nil, nil, err
	}

	// Allow all connections.
	_ = mqttServer.AddHook(new(auth.AllowHook), nil)

	// Create a new MQTT TCP listener on a standard port.
	tcp := listeners.NewTCP(listeners.Config{
		ID:      "tcp",
		Address: ":1883",
		// Address: mymqtt.Broker(log, true).Host,
	})

	err = mqttServer.AddListener(tcp)
	if err != nil {
		log.Error(err, "error adding TCP listener")
		return nil, nil, err
	}

	// start the mqtt server
	go func() {
		err := mqttServer.Serve()
		if err != nil {
			log.Error(err, "error starting MQTT server")
		}
	}()

	host, err := os.Hostname()
	if err != nil {
		log.Error(err, "error finding current hostname")
		return nil, nil, err
	}

	var instance string = program
	if program == "" {
		instance = host
	}

	// Register the service with mDNS.
	iface, _, err := mynet.MainInterface(log)
	ifaces := make([]net.Interface, 1)
	ifaces[0] = *iface
	if err != nil {
		log.Error(err, "Unable to get main local IP interface")
		return nil, nil, err
	}

	mdnsServer, err := zeroconf.Register(instance, mymqtt.ZEROCONF_SERVICE, "local.", mymqtt.PRIVATE_PORT, info, ifaces)
	if err != nil {
		log.Error(err, "Unable to register new ZeroConf service")
		return nil, nil, err
	}

	log.Info("Started new MQTT broker", "mdns_server", mdnsServer, "mdns_service", mymqtt.ZEROCONF_SERVICE)

	return mdnsServer, mymqtt.Broker(log, true), nil
}
