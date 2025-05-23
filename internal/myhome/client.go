package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"mymqtt"
	"pkg/devices"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

type client struct {
	log     logr.Logger
	to      chan<- []byte
	from    <-chan []byte
	me      string
	timeout time.Duration
}

func NewClientE(ctx context.Context, log logr.Logger, mc *mymqtt.Client, timeout time.Duration) (Client, error) {
	from, err := mc.Subscriber(ctx, ClientTopic(mc.Id()), 1)
	if err != nil {
		log.Error(err, "Failed to subscribe to client topic", "topic", ClientTopic(mc.Id()))
		return nil, err
	}

	to, err := mc.Publisher(ctx, ServerTopic(), 1)
	if err != nil {
		log.Error(err, "Failed to prepare publishing to server topic", "topic", ServerTopic())
		return nil, err
	}

	return &client{
		log:     log,
		from:    from,
		to:      to,
		me:      mc.Id(),
		timeout: timeout,
	}, nil
}

func (hc *client) Shutdown() {
	hc.log.Info("Shutting down client")
}

func (hc *client) LookupDevices(ctx context.Context, name string) (*[]devices.Device, error) {
	var out any
	var err error

	if strings.HasPrefix(name, "*") || strings.HasSuffix(name, "*") {
		out, err = TheClient.CallE(ctx, DevicesMatch, name)
	} else {
		out, err = TheClient.CallE(ctx, DeviceLookup, name)
	}
	if err != nil {
		return nil, err
	}

	mhd, ok := out.(*[]DeviceSummary)
	if !ok {
		return nil, fmt.Errorf("expected *[]myhome.DeviceSummary, got %T", out)
	}

	devices := make([]devices.Device, len(*mhd))
	for i, d := range *mhd {
		devices[i] = d
	}
	return &devices, nil
}

func (hc *client) ForgetDevices(ctx context.Context, name string) error {
	var err error

	devices, err := hc.LookupDevices(ctx, name)
	if err != nil {
		return err
	}

	for _, d := range *devices {
		_, err = TheClient.CallE(ctx, DeviceForget, d.Id())
		if err != nil {
			return err
		}
	}
	return nil
}

func (hc *client) CallE(ctx context.Context, method Verb, params any) (any, error) {
	requestId, err := RandStringBytesMaskImprRandReaderUnsafe(16)
	if err != nil {
		return nil, err
	}

	m, exists := signatures[method]
	if !exists {
		return Method{}, fmt.Errorf("unknown method %s", method)
	}

	if reflect.TypeOf(params) != reflect.TypeOf(m.NewParams()) {
		err := fmt.Errorf("invalid parameter type for method %s: got %v, should be %v", method, reflect.TypeOf(params), reflect.TypeOf(m.NewParams()))
		hc.log.Error(err, "Invalid parameter type")
		return nil, err
	}
	req := request{
		Dialog: Dialog{
			Id:  requestId,
			Src: hc.me,
			Dst: MYHOME,
		},
		Method: method,
		Params: params,
	}
	reqStr, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	if err := ValidateDialog(req.Dialog); err != nil {
		return nil, err
	}

	hc.log.Info("Calling method", "method", req.Method, "params", req.Params)
	hc.to <- reqStr

	var resStr []byte
	select {
	case <-ctx.Done():
		hc.log.Error(ctx.Err(), "Waiting for response to method", "method", req.Method)
		return nil, ctx.Err()
	case resStr = <-hc.from:
		hc.log.Info("Response", "payload", resStr)
		break
		// case <-time.After(hc.timeout):
		// 	return nil, fmt.Errorf("timed out waiting for response to method %s (%v)", method, hc.timeout)
	}

	var res response
	err = json.Unmarshal(resStr, &res)
	if err != nil {
		hc.log.Error(err, "Failed to unmarshal response", "payload", resStr)
		return nil, err
	}

	if err := ValidateDialog(res.Dialog); err != nil {
		hc.log.Error(err, "Invalid response dialog", "dialog", res.Dialog)
		return nil, err
	}

	rs, err := json.Marshal(res.Result)
	if err != nil {
		hc.log.Error(err, "Failed to re-marshal response.result", "result", res.Result)
		return nil, err
	}
	result := m.NewResult()
	hc.log.Info("Result", "type", reflect.TypeOf(result))
	err = json.Unmarshal(rs, &result)
	if err != nil {
		hc.log.Error(err, "Failed to re-unmarshal response.result", "payload", rs)
		return nil, err
	}

	if res.Error != nil {
		return nil, fmt.Errorf("%v (code:%v)", res.Error.Message, res.Error.Code)
	}

	return result, nil
}
