package mqtt

import (
	"devices/shelly/types"
	"encoding/json"
	"fmt"

	"mymqtt"
	"net/http"
	"os"
	"reflect"

	"github.com/go-logr/logr"
)

var registrar types.MethodsRegistrar

var log logr.Logger

type empty struct{}

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
	log.Info("Init package", reflect.TypeOf(empty{}).PkgPath())
	registrar = r
	r.RegisterMethodHandler("Mqtt", "GetStatus", types.MethodHandler{
		Allocate: func() any { return new(Status) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Mqtt", "GetConfig", types.MethodHandler{
		Allocate: func() any { return new(Configuration) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Mqtt", "SetConfig", types.MethodHandler{
		Allocate: func() any { return new(ConfigResults) },
		HttpQuery: map[string]string{
			"id": "0",
		},
		HttpMethod: http.MethodPost,
	})

	registrar.RegisterDeviceCaller(types.ChannelMqtt, types.DeviceCaller(mqttChannel.CallDevice))
}

type MqttChannel struct {
}

var mqttChannel MqttChannel

func (ch *MqttChannel) CallDevice(device types.Device, verb types.MethodHandler, out any, params any) (any, error) {
	reqTopic := fmt.Sprintf(" %v/rpc", device.Id())
	// reqChan, err := mqtt.MqttSubscribe(mqtt.PrivateBroker(), reqTopic, uint(AtLeastOnce))
	var req struct {
		Source string `json:"src"`
		Id     uint   `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}

	hostname, err := os.Hostname()
	if err != nil {
		log.Info("Unable to get local hostname: %v", err)
		return nil, err
	}
	req.Source = fmt.Sprintf("%v_%v", hostname, os.Getpid())
	req.Id = 0
	req.Method = verb.Method
	req.Params = params

	resChan, err := mymqtt.MqttSubscribe(log, mymqtt.Broker(log, false), fmt.Sprintf(" %v/rpc", req.Source), uint(AtLeastOnce))
	if err != nil {
		log.Info("Unable to subscribe to topic '%v': %v", reqTopic, err)
		return nil, err
	}

	reqPayload, err := json.Marshal(req)
	if err != nil {
		log.Info("Unable to marshal request payload '%v': %v", req, err)
		return nil, err
	}

	mymqtt.MqttPublish(log, mymqtt.Broker(log, false), reqTopic, reqPayload)
	res := <-resChan

	err = json.Unmarshal(res.Payload, &out)
	if err != nil {
		log.Info("Unable to unmarshal response payload '%v': %v", res, err)
		return nil, err
	}

	return out, nil
}
