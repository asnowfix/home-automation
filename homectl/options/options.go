package options

import (
	"context"
	"global"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-logr/logr"
)

var Flags struct {
	Verbose     bool
	Json        bool
	MqttBroker  string
	MqttTimeout time.Duration
	MqttGrace   time.Duration
	MdnsTimeout time.Duration
}

func CommandLineContext(log logr.Logger) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, global.LogKey, log)
	ctx, cancel := context.WithCancel(ctx)
	ctx = context.WithValue(ctx, global.CancelKey, cancel)
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		signal.Notify(signals, syscall.SIGTERM)
		<-signals
		log.Info("Received signal")
		cancel()
	}()
	return ctx
}

func SplitArgs(args []string) (before []string, after []string) {
	foundDelimiter := false
	for _, arg := range args {
		if arg == "--" {
			foundDelimiter = true
			continue
		}
		if foundDelimiter {
			after = append(after, arg)
		} else {
			before = append(before, arg)
		}
	}
	return
}
