package main

import (
	"devices/shelly/gen1"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hlog"

	"myhome/http"
	"myhome/logs"
	"myhome/mqtt"
)

var Program string
var Repo string
var Version string
var Commit string

func main() {
	hlog.Init()

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

	if len(Program) == 0 {
		Program = os.Args[0]
	}

	mdnsServer, broker, err := mqtt.MyHome(Program, info)
	if err != nil {
		log.Default().Printf("error starting MQTT server: %v", err)
	}
	defer mdnsServer.Shutdown()

	topicsCh := make(chan string, 1)
	defer close(topicsCh)
	go logs.Waiter(broker, topicsCh)

	gen1Ch := make(chan gen1.Device, 1)
	go http.MyHome(gen1Ch)
	go gen1.Publisher(gen1Ch, topicsCh, broker)

	proxyCh := make(chan struct{}, 1)
	go mqtt.CommandProxy(proxyCh)

	// Run server until interrupted
	<-done

	// Close command proxy channel
	close(proxyCh)

	// Cleanup
}
