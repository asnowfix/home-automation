package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"pkg/shelly/types"
	"time"

	"github.com/go-logr/logr"
)

// <https://shelly-api-docs.shelly.cloud/gen2/General/RPCChannels#mqtt>

var mqttChannel MqttChannel

type MqttChannel struct {
	log     *logr.Logger
	timeout time.Duration
}

func (ch *MqttChannel) Init(log logr.Logger, timeout time.Duration) {
	log = log.WithName("mqtt")
	ch.log = &log
	ch.timeout = timeout
	ch.log.Info("Init MQTT channel", "timeout", ch.timeout)
}

func (ch *MqttChannel) CallDevice(ctx context.Context, device types.Device, verb types.MethodHandler, out any, params any) (any, error) {
	var req Request

	req.Src = device.ReplyTo()
	req.Id = 0
	req.Method = verb.Method
	req.Params = params

	if req.Src == "" {
		panic("req.Src is empty")
	}

	reqPayload, err := json.Marshal(req)
	if err != nil {
		ch.log.Error(err, "Unable to marshal", "request", req)
		return nil, err
	}
	ch.log.Info("Sending to", "device", device.Id(), "request", req)
	device.To() <- reqPayload

	ch.log.Info("Waiting for response from", "device", device.Id(), "timeout", ch.timeout)
	var resMsg []byte
	select {
	case resMsg = <-device.From():
	case <-time.After(ch.timeout):
		ch.log.Error(nil, "Timeout waiting for response from", "device", device.Id(), "timeout", ch.timeout)
		return nil, fmt.Errorf("timeout waiting for response from %v", device.Id())
	}

	var res Response
	res.Result = &out

	err = json.Unmarshal(resMsg, &res)
	if err != nil {
		ch.log.Error(err, "Unable to unmarshal response", "payload", resMsg)
		return nil, err
	}

	ch.log.Info("Received", "response", res)
	if res.Error != nil {
		return nil, fmt.Errorf("%v (code:%v)", res.Error.Message, res.Error.Code)
	}

	return out, nil
}
