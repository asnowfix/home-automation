package shelly

import (
	"context"
	"fmt"
	"pkg/shelly/input"
	"pkg/shelly/kvs"
	"pkg/shelly/mqtt"
	"pkg/shelly/script"
	"pkg/shelly/shelly"
	shttp "pkg/shelly/shttp"
	"pkg/shelly/sswitch"
	"pkg/shelly/system"
	"pkg/shelly/types"
	"pkg/shelly/wifi"
	"reflect"
	"schedule"
	"time"

	"github.com/go-logr/logr"
)

type empty struct{}

func Init(log logr.Logger, timeout time.Duration) {
	log.Info("Init", "package", reflect.TypeOf(empty{}).PkgPath())
	registrar.Init(log)

	// Keep in lexical order
	// gen1.Init(log, &registrar)
	shelly.Init(log, &registrar, timeout)
	input.Init(log, &registrar)
	kvs.Init(log, &registrar)
	mqtt.Init(log, &registrar, timeout)
	schedule.Init(log, &registrar)
	script.Init(log, &registrar)
	shttp.Init(log, &registrar)
	sswitch.Init(log, &registrar)
	system.Init(log, &registrar)
	// temperature.Init(log, &registrar)
	wifi.Init(log, &registrar)
}

func (r *Registrar) CallE(ctx context.Context, d types.Device, via types.Channel, mh types.MethodHandler, params any) (any, error) {
	out := mh.Allocate()

	via = d.Channel(via)
	if via == types.ChannelDefault {
		return nil, fmt.Errorf("device %s (%s) has no possible comm channel", d.Id(), d.Name())
	}

	r.log.Info("Calling", "device_id", d.Id(), "method", mh.Method, "channel", via, "params", params, "out_type", reflect.TypeOf(out))
	return r.channels[via](ctx, d, mh, out, params)
}
