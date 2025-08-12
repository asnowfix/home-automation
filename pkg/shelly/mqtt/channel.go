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

	req.Id = device.StartDialog(ctx)
	req.Src = device.ReplyTo()
	req.Method = verb.Method
	req.Params = params

	if req.Src == "" {
		// FIXME: should not happen
		ch.log.Error(fmt.Errorf("req.Src is empty"), "Unable to call device", "device", device.Id(), "method", verb.Method)
		panic("req.Src is empty")
	}

	reqPayload, err := json.Marshal(req)
	if err != nil {
		ch.log.Error(err, "Unable to marshal", "request", req)
		return nil, err
	}
	// ch.log.Info("Sending to", "device", device.Id(), "request", req)
	device.To() <- reqPayload
	return ch.receiveResponse(ctx, device, verb.Method, req.Id, out)
}

func (ch *MqttChannel) receiveResponse(ctx context.Context, device types.Device, method string, reqId uint32, out any) (any, error) {
	// ch.log.Info("Waiting for response", "to verb", verb.Method, "from device", device.Id(), "timeout", ch.timeout)
	var resMsg []byte
	select {
	case resMsg = <-device.From():
		// ch.log.Info("Got response", "to verb", verb.Method, "from device", device.Id(), "response", string(resMsg))
	case <-time.After(ch.timeout):
		err := fmt.Errorf("timeout waiting for response from %s (%s)", device.Id(), device.Name())
		ch.log.Error(err, "Timeout waiting for device response", "to method", method, "id", device.Id(), "name", device.Name(), "timeout", ch.timeout)
		device.StopDialog(ctx, reqId)
		return nil, err
	}

	var res Response
	res.Result = &out

	err := json.Unmarshal(resMsg, &res)
	if err != nil {
		ch.log.Error(err, "Unable to unmarshal response", "payload", resMsg)
		return nil, err
	}

	device.StopDialog(ctx, reqId)
	if res.Id != reqId {
		err = fmt.Errorf("response.id (%v) does not match request.id (%v)", res.Id, reqId)
		ch.log.Error(err, "Dropping response", "out", out, "device", device.Id(), "method", method)
		return nil, err
		// out, err = ch.receiveResponse(ctx, device, method, reqId, out)
		// if err != nil {
		// 	return nil, err
		// }
	}

	if res.Error != nil {
		return nil, fmt.Errorf("device replied error '%v' (code:%v) to request '%v'", res.Error.Message, res.Error.Code, string(resMsg))
	}

	return out, nil
}
