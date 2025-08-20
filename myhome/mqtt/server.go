package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"mymqtt"
	"mynet"
	"net"
	"os"

	"github.com/go-logr/logr"

	mmqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/hooks/debug"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func MyHome(ctx context.Context, log logr.Logger, resolver mynet.Resolver, program string, info []string) error {
	log.Info("Starting MyHome", "program", program)

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
		log.Error(err, "error adding MQTT debug hook")
		return err
	}

	// Allow all connections.
	_ = mqttServer.AddHook(new(auth.AllowHook), nil)

	// Create a new MQTT TCP listener on a standard port.
	tcp := listeners.NewTCP(listeners.Config{
		ID:      "tcp",
		Address: fmt.Sprintf(":%d", mymqtt.PRIVATE_PORT),
		// Address: mymqtt.Broker(log, true).Host,
	})

	err = mqttServer.AddListener(tcp)
	if err != nil {
		log.Error(err, "error adding TCP listener")
		return err
	}

	err = mqttServer.Serve()
	if err != nil {
		log.Error(err, "error starting MQTT server")
		return err
	}

	host, err := os.Hostname()
	if err != nil {
		log.Error(err, "error finding current hostname")
		return err
	}

	var instance string = program
	if program == "" {
		instance = host
	}

	resolver.WithLocalName(ctx, mymqtt.HOSTNAME)

	// Register the MQTT broker service with mDNS.
	iface, _, err := mynet.MainInterface(log)
	if err != nil {
		log.Error(err, "Unable to get main local IP interface")
		return err
	}
	ifaces := make([]net.Interface, 1)
	ifaces[0] = *iface

	mdnsServer, err := resolver.PublishService(ctx, instance, mymqtt.ZEROCONF_SERVICE, "local.", mymqtt.PRIVATE_PORT, info, ifaces)
	if err != nil {
		log.Error(err, "Unable to register new ZeroConf service")
		return err
	}

	log.Info("Started new MQTT broker", "mdns_server", mdnsServer, "mdns_service", mymqtt.ZEROCONF_SERVICE)

	go func(ctx context.Context) {
		<-ctx.Done()
		mdnsServer.Shutdown()
		mqttServer.Close()
	}(ctx)

	log.Info("Started embedded MQTT broker & published it over mDNS/Zeroconf", "server", mdnsServer)

	return nil
}
