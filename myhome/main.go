package main

import (
	"devices/shelly/gen1"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"myhome/http"
	"myhome/mqtt"
)

var Program string
var Repo string
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
		fmt.Sprintf("repo=%v", Repo),
		fmt.Sprintf("version=%v", Version),
		fmt.Sprintf("commit=%v", Commit),
		fmt.Sprintf("time=%v", time.Now()),
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
