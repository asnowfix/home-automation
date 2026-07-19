package shelly

import (
	"context"
	"io/fs"
	"reflect"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/ble"
	"github.com/asnowfix/home-automation/pkg/shelly/ethernet"
	"github.com/asnowfix/home-automation/pkg/shelly/input"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	"github.com/asnowfix/home-automation/pkg/shelly/matter"
	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"
	"github.com/asnowfix/home-automation/pkg/shelly/ratelimit"
	"github.com/asnowfix/home-automation/pkg/shelly/schedule"
	"github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
	shttp "github.com/asnowfix/home-automation/pkg/shelly/shttp"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly/system"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/asnowfix/home-automation/pkg/shelly/wifi"

	"github.com/go-logr/logr"
)

type empty struct{}

// Init wires up every Shelly RPC sub-API (switches, KVS, scripts, WiFi,
// etc.) against registrar and mc. scriptsFS, if non-nil, is an embedded
// filesystem of house-specific automation scripts (garden, pool-pump,
// front-door…) that script.Init uses for version tracking; callers outside
// home-automation can pass nil to disable that feature entirely — pkg/shelly
// itself must not embed any one house's scripts (see CLAUDE.md's Three-Tier
// Layer Rule).
func Init(log logr.Logger, mc mqtt.Client, timeout time.Duration, rateLimitInterval time.Duration, scriptsFS fs.FS) {
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
	script.Init(log, &registrar, scriptsFS)
	shttp.Init(log, &registrar)
	sswitch.Init(log, &registrar)
	system.Init(log, &registrar)
	// temperature.Init(log, &registrar)
	wifi.Init(log, &registrar)
}

func (r *Registrar) CallE(ctx context.Context, d types.Device, via types.Channel, mh types.MethodHandler, params any) (any, error) {
	return r.channels[d.Channel(ctx, via)](ctx, d, mh, mh.Allocate(), params)
}

// SetHostResolver installs the resolver used to (re-)resolve a device's IP
// address when it is unknown, or immediately after an HTTP dial failure.
// See types.HostResolver. Call once at daemon startup.
func SetHostResolver(r types.HostResolver) {
	types.SetHostResolver(r)
}
