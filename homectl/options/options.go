package options

import (
	"context"
	"myhome"
	"mymqtt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var Flags struct {
	Verbose     bool
	ViaHttp     bool
	Json        bool
	Devices     string
	MqttBroker  string
	MqttTimeout time.Duration
	MqttGrace   time.Duration
}

var Devices []string

var MqttClient *mymqtt.Client

var MyHomeClient myhome.Client

func CommandLineContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		signal.Notify(signals, syscall.SIGTERM)
		<-signals
		cancel()
	}()
	return ctx
}
