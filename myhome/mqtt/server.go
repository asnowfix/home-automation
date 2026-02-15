package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"myhome/net"
	"net"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"

	mochimmqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/hooks/debug"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func Broker(ctx context.Context, log logr.Logger, resolver mynet.Resolver, program string, info []string, clientLogInterval time.Duration, noMdns bool) error {
	log.Info("Starting MyHome", "program", program)

	// Create the new MQTT Server.
	mqttServer := mochimmqtt.New(nil)

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
		Address: fmt.Sprintf(":%d", PRIVATE_PORT),
		// Address: Broker(log, true).Host,
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

	var mdnsServer *zeroconf.Server
	if !noMdns {
		resolver.WithLocalName(ctx, HOSTNAME)

		// Register the MQTT broker service with mDNS.
		iface, _, err := mynet.MainInterface(log)
		if err != nil {
			log.Error(err, "Unable to get main local IP interface")
			return err
		}
		ifaces := make([]net.Interface, 1)
		ifaces[0] = *iface

		mdnsServer, err = resolver.PublishService(ctx, instance, ZEROCONF_SERVICE, "local.", PRIVATE_PORT, info, ifaces)
		if err != nil {
			log.Error(err, "Unable to register new ZeroConf service")
			return err
		}

		log.Info("Started new MQTT broker", "mdns_server", mdnsServer, "mdns_service", ZEROCONF_SERVICE)
	} else {
		log.Info("Started new MQTT broker (mDNS disabled)")
	}

	// Start periodic client monitoring if enabled
	if clientLogInterval > 0 {
		go func(log logr.Logger) {
			ticker := time.NewTicker(clientLogInterval)
			defer ticker.Stop()

			log.Info("Starting MQTT broker client monitoring", "interval", clientLogInterval)

			for {
				select {
				case <-ctx.Done():
					log.Info("Stopping MQTT broker client monitoring")
					return
				case <-ticker.C:
					// Log connected clients
					clients := mqttServer.Clients.GetAll()
					clientIds := make([]string, 0, len(clients))
					for id := range clients {
						clientIds = append(clientIds, id)
					}
					log.Info("MQTT broker connected clients", "count", len(clients), "client_ids", clientIds)
				}
			}
		}(log.WithName("monitor"))
	} else {
		log.Info("MQTT broker client monitoring disabled")
	}

	go func(log logr.Logger) {
		<-ctx.Done()
		log.Info("Shutting down MQTT broker")
		if mdnsServer != nil {
			mdnsServer.Shutdown()
		}
		mqttServer.Close()
	}(log.WithName("cleanup"))

	if !noMdns {
		log.Info("Started embedded MQTT broker & published it over mDNS/Zeroconf", "server", mdnsServer)
	}

	return nil
}
