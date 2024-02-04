package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/mdns"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

var Program string
var Version string
var Commit string

func main() {
	// Create signals channel to run server until interrupted
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

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

	// Publish over mDNS
	host, _ := os.Hostname()
	service := "_mqtt._tcp"
	info := []string{
		fmt.Sprintf("hostname=%v", host),
		fmt.Sprintf("program=%v", Program),
		fmt.Sprintf("version=%v", Version),
		fmt.Sprintf("commit=%v", Commit),
	}
	mdnsService, _ := mdns.NewMDNSService(host, service, "", "", 1883, nil, info)
	log.Default().Printf("publishing %v as %v over mDNS", info, service)

	// Create the mDNS server, defer shutdown
	mdnsServer, _ := mdns.NewServer(&mdns.Config{Zone: mdnsService})
	defer mdnsServer.Shutdown()

	go func() {
		err := mqttServer.Serve()
		if err != nil {
			log.Fatal(err)
		}
	}()

	// Run server until interrupted
	<-done

	// Cleanup
}
