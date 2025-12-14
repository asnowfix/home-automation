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

const COMMAND_DEFAULT_TIMEOUT time.Duration = 0 // No timeout by default (wait indefinitely)

const DEVICE_REFRESH_INTERVAL time.Duration = 1 * time.Minute

const MQTT_WATCHDOG_CHECK_INTERVAL time.Duration = 30 * time.Second

const MQTT_WATCHDOG_MAX_FAILURES int = 3

const MQTT_BROKER_CLIENT_LOG_INTERVAL time.Duration = 2 * time.Minute

const SHELLY_DEFAULT_RATE_LIMIT time.Duration = 500 * time.Millisecond

var Flags struct {
	CpuProfile                  string
	Verbose                     bool
	Debug                       bool
	Quiet                       bool
	Json                        bool
	MqttBroker                  string
	MqttTimeout                 time.Duration // the value taken by --mqtt-timeout / -T
	MqttGrace                   time.Duration // the value taken by --mqtt-grace / -G
	MdnsTimeout                 time.Duration // the value taken by --mdns-timeout / -M
	Wait                        time.Duration // the value taken by --command-timeout / -C
	RefreshInterval             time.Duration // the value taken by --refresh-interval / -R
	MqttWatchdogInterval        time.Duration // the value taken by --mqtt-watchdog-interval
	MqttWatchdogMaxFailures     int           // the value taken by --mqtt-watchdog-max-failures
	MqttBrokerClientLogInterval time.Duration // the value taken by --mqtt-broker-client-log-interval
	Via                         string
	SwitchId                    uint32
	EventsDir                   string
	ProxyPort                   int
	EnableGen1Proxy             bool
	EnableOccupancyService      bool
	EnableTemperatureService    bool
	EnableMetricsExporter       bool
	MetricsExporterPort         int
	MetricsExporterTopic        string
	ShellyRateLimit             time.Duration // the value taken by --shelly-rate-limit
}

var Via types.Channel

func CommandLineContext(ctx context.Context, version string) context.Context {
	var cancel context.CancelFunc

	// Create the process-wide context that background services can use
	processCtx, processCancel := context.WithCancel(ctx)

	if Flags.Wait > 0 {
		ctx, cancel = context.WithTimeout(processCtx, Flags.Wait)
		ctx = context.WithValue(ctx, global.CancelKey, cancel)
	} else {
		ctx, cancel = context.WithCancel(processCtx)
		ctx = context.WithValue(ctx, global.CancelKey, cancel)
	}

	// Store the process-wide context so lazy-started services can access it
	ctx = context.WithValue(ctx, global.ProcessContextKey, processCtx)
	ctx = context.WithValue(ctx, global.VersionKey, version)

	go func() {
		log := logr.FromContextOrDiscard(ctx)
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		signal.Notify(signals, syscall.SIGTERM)
		<-signals
		log.Info("Received signal")
		// Cancel both the operation context and the process context
		cancel()
		processCancel()
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
