package main

import (
	"devices/shelly/gen1"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"myhome/http"
	"myhome/mqtt"
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

	// Publish MQTT server info over mDNS
	info := []string{
		fmt.Sprintf("program=%v", Program),
		fmt.Sprintf("version=%v", Version),
		fmt.Sprintf("commit=%v", Commit),
	}

	mdnsServer, _ := mqtt.MyHome(info)
	defer mdnsServer.Shutdown()

	tc := make(chan gen1.Device, 1)
	go http.MyHome(tc)
	go gen1.Publisher(tc)

	// Run server until interrupted
	<-done

	// Cleanup
}
