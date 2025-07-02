package shelly

import (
	"fmt"
	"pkg/shelly/types"

	"github.com/go-logr/logr"
)

var registrar Registrar

// return singleton registrar
func GetRegistrar() *Registrar {
	return &registrar
}

type Registrar struct {
	log      logr.Logger
	methods  map[string]types.MethodHandler
	channel  types.Channel
	channels []types.DeviceCaller
}

func (r *Registrar) Init(log logr.Logger) {
	r.log = log
	r.channel = types.ChannelHttp
	r.channels = make([]types.DeviceCaller, 3 /*sizeof(Channel)*/)
	r.methods = make(map[string]types.MethodHandler)
}

func (r *Registrar) MethodHandlerE(m string) (types.MethodHandler, error) {
	mh, ok := r.methods[m]
	if !ok {
		return types.MethodHandler{}, fmt.Errorf("method not found in registrar: %s", m)
	}
	return mh, nil
}

// func (r *Registrar) MethodHandler(m string) types.MethodHandler {
// 	return r.methods[m]
// }

func (r *Registrar) RegisterMethodHandler(verb string, mh types.MethodHandler) {
	// r.log.Info("Registering", "method", verb)
	if _, exists := r.methods[verb]; exists {
		panic(fmt.Errorf("method %s already registered", verb))
	}
	mh.Method = verb
	if mh.Allocate == nil {
		mh.Allocate = func() any {
			return make(map[string]interface{})
		}
	}
	r.methods[verb] = mh
}

func (r *Registrar) RegisterDeviceCaller(ch types.Channel, dc types.DeviceCaller) {
	r.log.Info("Registering", "channel", ch, "caller", dc)

	// err := errors.Wrap(fmt.Errorf("stack registering %v for channel %s", dc, ch), "registering")
	// r.log.Error(err, "Registering", "channel", ch, "caller", dc)

	r.channels[ch] = dc
}
