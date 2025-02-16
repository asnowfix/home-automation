package options

import (
	"context"
	"myhome"
	"mymqtt"
	"os"
	"os/signal"
	"time"
)

var Flags struct {
	Verbose     bool
	ViaHttp     bool
	Json        bool
	Devices     string
	MqttBroker  string
	MqttTimeout time.Duration
}

var Devices []string

var MqttClient *mymqtt.Client

var MyHomeClient myhome.Client

func InterruptibleContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		<-signals
		cancel()
	}()
	return ctx, cancel
}
