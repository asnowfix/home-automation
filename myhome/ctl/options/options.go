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

const MDNS_LOOKUP_DEFAULT_TIMEOUT time.Duration = 7 * time.Second

const MQTT_DEFAULT_TIMEOUT time.Duration = 14 * time.Second

const MQTT_DEFAULT_GRACE time.Duration = 2 * time.Second

const COMMAND_DEFAULT_TIMEOUT time.Duration = 15 * time.Second

const DEVICE_REFRESH_INTERVAL time.Duration = 1 * time.Minute

const MQTT_WATCHDOG_CHECK_INTERVAL time.Duration = 30 * time.Second

const MQTT_WATCHDOG_MAX_FAILURES int = 3

var Flags struct {
	CpuProfile              string
	Verbose                 bool
	Quiet                   bool
	Json                    bool
	MqttBroker              string
	MqttTimeout             time.Duration // the value taken by --mqtt-timeout / -T
	MqttGrace               time.Duration // the value taken by --mqtt-grace / -G
	MdnsTimeout             time.Duration // the value taken by --mdns-timeout / -M
	Wait                    time.Duration // the value taken by --command-timeout / -C
	RefreshInterval         time.Duration // the value taken by --refresh-interval / -R
	MqttWatchdogInterval    time.Duration // the value taken by --mqtt-watchdog-interval
	MqttWatchdogMaxFailures int           // the value taken by --mqtt-watchdog-max-failures
	Via                     string
	SwitchId                uint32
	EventsDir               string
	ProxyPort               int
	EnableGen1Proxy         bool
	EnableOccupancyService  bool
}

var Via types.Channel

func CommandLineContext(ctx context.Context, version string) context.Context {
	var cancel context.CancelFunc

	if Flags.Wait > 0 {
		ctx, cancel = context.WithTimeout(ctx, Flags.Wait)
		ctx = context.WithValue(ctx, global.CancelKey, cancel)
	} else {
		ctx, cancel = context.WithCancel(ctx)
		ctx = context.WithValue(ctx, global.CancelKey, cancel)
	}

	ctx = context.WithValue(ctx, global.VersionKey, version)

	go func() {
		log := logr.FromContextOrDiscard(ctx)
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
