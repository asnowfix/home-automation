package mqtt

import (
	"devices/shelly/types"
	"encoding/json"
	"fmt"
	"time"

	"mymqtt"
	"net/http"
	"os"
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

func requestId() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

var mqttChannel MqttChannel

type MqttChannel struct {
}

func (ch *MqttChannel) CallDevice(device types.Device, verb types.MethodHandler, out any, params any) (any, error) {
	reqTopic := fmt.Sprintf("%v/rpc", device.Id())
	// reqChan, err := mqtt.MqttSubscribe(mqtt.PrivateBroker(), reqTopic, uint(AtLeastOnce))
	var req struct {
		Source string `json:"src"`
		Id     uint   `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}

	hostname, err := os.Hostname()
	if err != nil {
		log.Error(err, "Unable to get local hostname")
		return nil, err
	}
	req.Source = fmt.Sprintf("%v_%v", hostname, requestId())
	req.Id = 0
	req.Method = verb.Method
	req.Params = params

	resTopic := fmt.Sprintf("%v/rpc", req.Source)

	resChan, err := mymqtt.MqttSubscribe(log, mymqtt.Broker(log, false), resTopic, uint(AtLeastOnce))
	if err != nil {
		log.Error(err, "Unable to subscribe", "topic", reqTopic)
		return nil, err
	}
	log.Info("Subscribing...", "topic", resTopic)
	<-resChan // subscribed
	log.Info("Subscribed", "topic", resTopic)

	reqPayload, err := json.Marshal(req)
	if err != nil {
		log.Error(err, "Unable to marshal", "request", req)
		return nil, err
	}
	mymqtt.MqttPublish(log, mymqtt.Broker(log, false), reqTopic, reqPayload)
	resMsg := <-resChan

	mymqtt.MqttUnsubscribe(log, mymqtt.Broker(log, false), resTopic)

	var res struct {
		Id     uint   `json:"id"`
		Src    string `json:"src"`
		Dst    string `json:"dst"`
		Result *any   `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	res.Result = &out

	err = json.Unmarshal(resMsg.Payload, &res)
	if err != nil {
		log.Error(err, "Unable to unmarshal response", "payload", resMsg.Payload)
		return nil, err
	}

	log.Info("Received", "response", res)
	if res.Error != nil {
		return nil, fmt.Errorf("%v (code:%v)", res.Error.Message, res.Error.Code)
	}

	return out, nil
}