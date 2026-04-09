package shelly

import (
	"context"
	"github.com/asnowfix/home-automation/pkg/shelly/ble"
	"github.com/asnowfix/home-automation/pkg/shelly/ethernet"
	"github.com/asnowfix/home-automation/pkg/shelly/input"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	"github.com/asnowfix/home-automation/pkg/shelly/matter"
	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/ratelimit"
	"github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
	shttp "github.com/asnowfix/home-automation/pkg/shelly/shttp"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly/system"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/asnowfix/home-automation/pkg/shelly/wifi"
	"reflect"
	"github.com/asnowfix/home-automation/pkg/shelly/schedule"
	scripts "github.com/asnowfix/home-automation/internal/shelly/scripts"
	"time"

	"github.com/go-logr/logr"
)

type empty struct{}

func Init(log logr.Logger, mc mqtt.Client, timeout time.Duration, rateLimitInterval time.Duration) {
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
	mqtt.Init(log, &registrar, mc, timeout)
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
