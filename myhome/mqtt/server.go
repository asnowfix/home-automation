package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	mynet "github.com/asnowfix/home-automation/internal/myhome/net"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
	"github.com/spf13/viper"

	mochiServer "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/hooks/debug"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func Broker(ctx context.Context, log logr.Logger, resolver mynet.Resolver, program string, info []string, clientLogInterval time.Duration, noMdns bool, v *viper.Viper, clientWritesPending int) error {
	log = log.WithName("MqttBroker")
	log.Info("Starting MyHome", "program", program, "info", info, "clientLogInterval", clientLogInterval, "noMdns", noMdns)

	// Load broker configuration from Viper (or use defaults)
	opts := loadBrokerConfig(ctx, log, v, clientWritesPending)

	// Logger specifies a custom configured implementation of log/slog to override
	// the servers default logger configuration. If you wish to change the log level,
	// of the default logger, you can do so by setting:
	// server := mqtt.New(nil)
	// level := new(slog.LevelVar)
	// server.Slog = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	//  Level: level,
	// }))
	// level.Set(slog.LevelDebug)
	// Override logger to use our logr-based logger
	opts.Logger = slog.New(logr.ToSlogHandler(log))

	// Enable inline client so that we can publish & subscribe from the same process without the need for the network overhead
	opts.InlineClient = true

	log.Info("Creating Mochi MQTT server instance")
	mqttServer := mochiServer.New(opts)

	log.Info("Mochi MQTT server instance created")

	log.Info("Adding debug hook")
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
	log.Info("Debug hook added")

	// Make dropped messages (client outbound queue full) visible
	if err := mqttServer.AddHook(newDropLogger(log.WithName("drops"), dropLogInterval, time.Now), nil); err != nil {
		log.Error(err, "error adding MQTT dropped-message hook")
		return err
	}

	// Allow all connections.
	log.Info("Adding auth hook")
	_ = mqttServer.AddHook(new(auth.AllowHook), nil)
	log.Info("Auth hook added")

	// Create a new MQTT TCP listener on a standard port.
	// Bind to 0.0.0.0 to listen on all interfaces including loopback
	log.Info("Creating TCP listener", "port", PRIVATE_PORT, "address", "0.0.0.0")
	tcp := listeners.NewTCP(listeners.Config{
		ID:      "tcp",
		Address: fmt.Sprintf("0.0.0.0:%d", PRIVATE_PORT),
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
	log.Info("Now listening for MQTT connections", "tcp", tcp)

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
		log.Info("Publishing MQTT broker over mDNS")
		// Register local hostname and MQTT broker service with mDNS
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

		log.Info("Published MQTT broker over mDNS", "mdns_server", mdnsServer, "mdns_service", ZEROCONF_SERVICE)
	} else {
		log.Info("MQTT broker has mDNS disabled")
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
					log.Info("MQTT broker connected clients", "count", len(clients), "client_ids", clientIds,
						"messages_dropped", atomic.LoadInt64(&mqttServer.Info.MessagesDropped))
				}
			}
		}(log.WithName("monitor"))
	} else {
		log.Info("MQTT broker client monitoring disabled")
	}

	// Start periodic stale client cleanup task
	// This forcibly disconnects clients that are already closed but still in the client list
	go func(log logr.Logger) {
		// Run cleanup every 2 minutes
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()

		log.Info("Starting MQTT broker stale client cleanup", "check_interval", 2*time.Minute)

		for {
			select {
			case <-ctx.Done():
				log.Info("Stopping MQTT broker stale client cleanup")
				return
			case <-ticker.C:
				staleCount := removeClosedClients(log, mqttServer.Clients, mqttServer.Clients.GetAll())

				if staleCount > 0 {
					// Get fresh count after deletions (clients variable is a snapshot from before deletions)
					remainingClients := len(mqttServer.Clients.GetAll())
					log.Info("Stale client cleanup completed", "removed_count", staleCount, "remaining_clients", remainingClients)
				}
			}
		}
	}(log.WithName("cleanup-stale"))

	go func(log logr.Logger) {
		<-ctx.Done()
		log.Info("Shutting down MQTT broker")
		if mdnsServer != nil {
			mdnsServer.Shutdown()
		}
		mqttServer.Close()
	}(log.WithName("cleanup"))

	return nil
}

// removeClosedClients deletes from the broker's client registry every client of snapshot that is
// closed, and returns how many it deleted. snapshot is a copy of the registry taken earlier
// (Clients.GetAll()), so it is passed in explicitly: that is what lets a test reproduce a client
// reconnecting between the snapshot and the deletion (#858).
//
// Deleting a registry entry does not touch Mochi's topic index: subscriptions stay indexed by
// client id, so a later connection with the same id keeps receiving them (Mochi resolves the id
// through the registry at delivery time).
func removeClosedClients(log logr.Logger, registry *mochiServer.Clients, snapshot map[string]*mochiServer.Client) int {
	removed := 0
	for id, client := range snapshot {
		// Closed() returns true if the client has been stopped
		if !client.Closed() {
			continue
		}
		log.Info("Removing closed client from registry",
			"client_id", id,
			"stop_time", time.Unix(client.StopTime(), 0))

		// The client is already stopped, we just need to clean up the registry
		registry.Delete(id)
		removed++
	}
	return removed
}
