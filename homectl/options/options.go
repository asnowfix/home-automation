package options

import (
	"context"
	"encoding/json"
	"fmt"
	"global"
	"os"
	"os/signal"
	"pkg/shelly/types"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"
)

const MDNS_LOOKUP_TIMEOUT time.Duration = 7 * time.Second

const MQTT_DEFAULT_TIMEOUT time.Duration = 14 * time.Second

const MQTT_DEFAULT_GRACE time.Duration = 2 * time.Second

const COMMAND_TIMEOUT = 15 * time.Second

var Flags struct {
	CpuProfile     string
	Verbose        bool
	Json           bool
	MqttBroker     string
	MqttTimeout    time.Duration
	MqttGrace      time.Duration
	MdnsTimeout    time.Duration
	CommandTimeout time.Duration
	Via            string
	SwitchId       uint32
}

var Via types.Channel

func CommandLineContext(ctx context.Context, log logr.Logger, timeout time.Duration) context.Context {
	ctx = context.WithValue(ctx, global.LogKey, log)
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		ctx = context.WithValue(ctx, global.CancelKey, cancel)
	} else {
		ctx, cancel = context.WithCancel(ctx)
		ctx = context.WithValue(ctx, global.CancelKey, cancel)
	}
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

func Args(args []string) []string {
	if len(args) > 1 {
		return args[1:]
	}
	return make([]string, 0)
}

func PrintResult(out any) error {
	if Flags.Json {
		s, err := json.Marshal(out)
		if err != nil {
			return err
		}
		fmt.Println(string(s))
	} else {
		s, err := yaml.Marshal(out)
		if err != nil {
			return err
		}
		fmt.Println(string(s))
	}
	return nil
}
