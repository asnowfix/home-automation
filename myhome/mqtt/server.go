package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	mynet "myhome/net"
	"net"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
	"github.com/spf13/viper"

	mochimmqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/hooks/debug"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func Broker(ctx context.Context, log logr.Logger, resolver mynet.Resolver, program string, info []string, clientLogInterval time.Duration, noMdns bool, v *viper.Viper) error {
	log.Info("Starting MyHome", "program", program)

	// // Load broker configuration from Viper or use defaults
	// cfg := LoadBrokerConfig(v)
	// log.Info("MQTT broker configuration",
	// 	"maximum_session_expiry_interval", cfg.MaximumSessionExpiryInterval,
	// 	"maximum_message_expiry_interval", cfg.MaximumMessageExpiryInterval,
	// 	"receive_maximum", cfg.ReceiveMaximum,
	// 	"maximum_qos", cfg.MaximumQos)

	// // Create the new MQTT Server with custom options
	// opts := &mochimmqtt.Options{
	// 	Capabilities: &mochimmqtt.Capabilities{
	// 		MaximumSessionExpiryInterval: cfg.MaximumSessionExpiryInterval,
	// 		MaximumMessageExpiryInterval: cfg.MaximumMessageExpiryInterval,
	// 		ReceiveMaximum:               cfg.ReceiveMaximum,
	// 		MaximumQos:                   cfg.MaximumQos,
	// 		RetainAvailable:              cfg.RetainAvailable,
	// 		WildcardSubAvailable:         cfg.WildcardSubAvailable,
	// 		SubIDAvailable:               cfg.SubIDAvailable,
	// 		SharedSubAvailable:           cfg.SharedSubAvailable,
	// 	},
	// }
	// mqttServer := mochimmqtt.New(opts)

	// Create the new MQTT Server with default options
	// Note: Providing a partial Capabilities struct causes "server busy" errors
	// Mochi MQTT requires either nil (use all defaults) or a fully initialized Capabilities struct
	log.Info("Creating Mochi MQTT server instance with default options")
	mqttServer := mochimmqtt.New(nil)

	log.Info("Mochi MQTT server instance created")

	// Configure the MQTT server so that every message is logged.
	log.Info("Configuring MQTT server logger")
	mqttServer.Log = slog.New(logr.ToSlogHandler(log))

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
	log.Info("TCP listener created")

	log.Info("Adding TCP listener to server")
	err = mqttServer.AddListener(tcp)
	if err != nil {
		log.Error(err, "error adding TCP listener")
		return err
	}
	log.Info("TCP listener added to server")

	log.Info("Calling mqttServer.Serve()")
	err = mqttServer.Serve()
	if err != nil {
		log.Error(err, "error starting MQTT server")
		return err
	}
	log.Info("mqttServer.Serve() returned successfully (listeners starting in background)")

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

		log.Info("Started new MQTT broker", "mdns_server", mdnsServer, "mdns_service", ZEROCONF_SERVICE)

		// Give mDNS a moment to propagate before clients try to connect
		time.Sleep(500 * time.Millisecond)
		log.Info("MQTT broker ready for client connections")
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
