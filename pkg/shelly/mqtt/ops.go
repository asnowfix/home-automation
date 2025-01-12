package mqtt

import (
	"encoding/json"
	"fmt"
	"pkg/shelly/types"
	"time"

	"net/http"
	"reflect"

	"github.com/go-logr/logr"
	"golang.org/x/exp/rand"
)

var registrar types.MethodsRegistrar

var log logr.Logger

type empty struct{}

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
	log.Info("Init package", reflect.TypeOf(empty{}).PkgPath())
	registrar = r
	r.RegisterMethodHandler("Mqtt", "GetStatus", types.MethodHandler{
		Allocate:   func() any { return new(Status) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Mqtt", "GetConfig", types.MethodHandler{
		Allocate:   func() any { return new(Configuration) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Mqtt", "SetConfig", types.MethodHandler{
		Allocate:   func() any { return new(ConfigResults) },
		HttpMethod: http.MethodPost,
	})

	registrar.RegisterDeviceCaller(types.ChannelMqtt, types.DeviceCaller(mqttChannel.CallDevice))
}

func init() {
	rand.Seed(uint64(time.Now().UnixNano()))
}

var mqttChannel MqttChannel

type MqttChannel struct {
}

func (ch *MqttChannel) CallDevice(device types.Device, verb types.MethodHandler, out any, params any) (any, error) {
	var req Request

	req.Src = device.ReplyTo()
	req.Id = 0
	req.Method = verb.Method
	req.Params = params

	reqPayload, err := json.Marshal(req)
	if err != nil {
		log.Error(err, "Unable to marshal", "request", req)
		return nil, err
	}
	log.Info("Sending", "request", req)
	device.To() <- reqPayload

	log.Info("Waiting for response")
	resMsg := <-device.From()

	var res Response
	res.Result = &out

	err = json.Unmarshal(resMsg, &res)
	if err != nil {
		log.Error(err, "Unable to unmarshal response", "payload", resMsg)
		return nil, err
	}

	log.Info("Received", "response", res)
	if res.Error != nil {
		return nil, fmt.Errorf("%v (code:%v)", res.Error.Message, res.Error.Code)
	}

	return out, nil
}
