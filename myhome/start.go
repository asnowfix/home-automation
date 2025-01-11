package main

import (
	"fmt"
	"hlog"
	"myhome/http"
	"myhome/mqtt"
	"mymqtt"
	"os"
	"os/signal"
	"pkg/shelly/gen1"
	"syscall"
	"time"
)

func start() {
	log := hlog.Init()

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

	mdnsServer, _, err := mqtt.MyHome(log, Program, info)
	if err != nil {
		log.Error(err, "error starting MQTT server")
	}
	defer mdnsServer.Shutdown()

	client, err := mymqtt.NewClientE(log, "me")
	if err != nil {
		log.Error(err, "error starting MQTT client")
	}
	defer client.Close()

	gen1Ch := make(chan gen1.Device, 1)
	go http.MyHome(log, gen1Ch)
	go gen1.Publisher(log, gen1Ch, client)

	// Run server until interrupted
	<-done
}
