package main

import (
	"devices/shelly"
	"devices/shelly/gen1"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hlog"

	"myhome/http"
	"myhome/logs"
	"myhome/mqtt"

	"github.com/asnowfix/home-automation/myhome/devices"
	"github.com/go-logr/logr"
	"github.com/grandcat/zeroconf"
)

var Program string
var Repo string
var Version string
var Commit string

func main() {
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

	mdnsServer, broker, err := mqtt.MyHome(log, Program, info)
	if err != nil {
		log.Error(err, "error starting MQTT server")
		return
	}
	log.Info("Started MQTT server & published it over mDNS/Zeroconf", "server", mdnsServer)
	defer mdnsServer.Shutdown()

	topicsCh := make(chan string, 1)
	defer close(topicsCh)
	go logs.Waiter(log, broker, topicsCh)

	gen1Ch := make(chan gen1.Device, 1)
	go http.MyHome(log, gen1Ch)
	go gen1.Publisher(log, gen1Ch, topicsCh, broker)

	proxyCh := make(chan struct{}, 1)
	go mqtt.CommandProxy(log, proxyCh)
	defer close(proxyCh)

	ds, err := devices.NewDeviceStorage(log, "myhome.db")
	// fails startup if storage fails to start
	if err != nil {
		log.Error(err, "error starting device storage")
		return
	}
	log.Info("Started device storage", "storage", ds)
	defer ds.Close()

	dm := devices.NewDeviceManager(log, ds)
	log.Info("Started device manager", "manager", dm)

	shelly.Init(log)
	dm.DiscoverDevices(shelly.MDNS_SHELLIES, 5*time.Second, func(log logr.Logger, entry *zeroconf.ServiceEntry) (*devices.DeviceIdentifier, error) {
		log.Info("Identifying", "entry", entry)
		return &devices.DeviceIdentifier{
			Manufacturer: "Shelly",
			ID:           entry.Instance,
		}, nil
	}, func(log logr.Logger, entry *zeroconf.ServiceEntry) (*devices.Device, error) {
		sd, err := shelly.NewDeviceFromZeroConfEntry(log, entry)
		if err != nil {
			return nil, err
		}
		log.Info("Got", "shelly_device", sd)
		return &devices.Device{
			DeviceIdentifier: devices.DeviceIdentifier{
				Manufacturer: "Shelly",
				ID:           sd.Id_,
			},
			MAC:  sd.MacAddress,
			Host: sd.Ipv4_.String(),
		}, nil
	})
	defer dm.StopDiscovery()

	// Run server until interrupted
	<-done
	log.Info("Shutting down")
}
