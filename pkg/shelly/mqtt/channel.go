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
	req.Id = 0 // TODO: implement correlation
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
	// ch.log.Info("Sending to", "device", device.Id(), "request", req)
	device.To() <- reqPayload

	// ch.log.Info("Waiting for response", "to verb", verb.Method, "from device", device.Id(), "timeout", ch.timeout)
	var resMsg []byte
	select {
	case resMsg = <-device.From():
		// ch.log.Info("Got response", "to verb", verb.Method, "from device", device.Id(), "response", string(resMsg))
	case <-time.After(ch.timeout):
		err := fmt.Errorf("timeout waiting for response from %s", device.String())
		ch.log.Error(err, "Timeout waiting for device response", "to verb", verb.Method, "from device", device.String(), "timeout", ch.timeout)
		device.MqttOk(false)
		return nil, err
	}

	var res Response
	res.Result = &out

	err = json.Unmarshal(resMsg, &res)
	if err != nil {
		ch.log.Error(err, "Unable to unmarshal response", "payload", resMsg)
		return nil, err
	}

	// ch.log.Info("Received", "response", res)
	if res.Error != nil {
		return nil, fmt.Errorf("device replied error '%v' (code:%v) to request '%v'", res.Error.Message, res.Error.Code, string(reqPayload))
	}

	device.MqttOk(true)
	return out, nil
}
