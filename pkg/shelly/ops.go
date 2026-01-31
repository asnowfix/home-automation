package shelly

import (
	"context"
	"pkg/shelly/ble"
	"pkg/shelly/ethernet"
	"pkg/shelly/input"
	"pkg/shelly/kvs"
	"pkg/shelly/matter"
	"pkg/shelly/mqtt"
	"pkg/shelly/ratelimit"
	"pkg/shelly/script"
	"pkg/shelly/shelly"
	shttp "pkg/shelly/shttp"
	"pkg/shelly/sswitch"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"
	"reflect"
	"schedule"
	scripts "shelly/scripts"
	"time"

	"github.com/go-logr/logr"
)

type empty struct{}

func Init(log logr.Logger, timeout time.Duration, rateLimitInterval time.Duration) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath(), "rateLimit", rateLimitInterval)
	registrar.Init(log)

	// Initialize rate limiter
	ratelimit.Init(rateLimitInterval)

	// Keep in lexical order
	// gen1.Init(log, &registrar)
	shelly.Init(log, &registrar, timeout)
	ble.Init(log, &registrar)
	ethernet.Init(log, &registrar)
	input.Init(log, &registrar)
	kvs.Init(log, &registrar)
	matter.Init(log, &registrar)
	mqtt.Init(log, &registrar, timeout)
	schedule.Init(log, &registrar)
	script.Init(log, &registrar, scripts.GetFS())
	shttp.Init(log, &registrar)
	sswitch.Init(log, &registrar)
	system.Init(log, &registrar)
	// temperature.Init(log, &registrar)
	wifi.Init(log, &registrar)
}

func (r *Registrar) CallE(ctx context.Context, d types.Device, via types.Channel, mh types.MethodHandler, params any) (any, error) {
	return r.channels[d.Channel(via)](ctx, d, mh, mh.Allocate(), params)
}
