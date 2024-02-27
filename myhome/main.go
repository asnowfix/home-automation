package main

import (
	"devices/shelly/gen1"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"myhome/http"
	"myhome/logs"
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

	mdnsServer, _ := mqtt.MyHome(Program, info)
	defer mdnsServer.Shutdown()

	topicsCh := make(chan string, 1)
	defer close(topicsCh)
	go logs.Waiter(topicsCh)

	gen1Ch := make(chan gen1.Device, 1)
	go http.MyHome(gen1Ch)
	go gen1.Publisher(gen1Ch, topicsCh)

	// Run server until interrupted
	<-done

	// Cleanup
}
