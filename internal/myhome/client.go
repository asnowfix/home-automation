package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"myhome/mqtt"
	mynet "myhome/net"
	"net"
	"pkg/devices"
	"pkg/shelly"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

type client struct {
	lock    sync.Mutex
	log     logr.Logger
	to      chan<- []byte
	from    <-chan []byte
	me      string
	timeout time.Duration
	mc      mqtt.Client
}

func NewClientE(ctx context.Context, log logr.Logger, mc mqtt.Client, timeout time.Duration) (Client, error) {
	c := &client{
		log:     log,
		timeout: timeout,
		mc:      mc,
	}
	return c, nil
}

func (hc *client) start(ctx context.Context) {
	var err error

	hc.lock.Lock()
	defer hc.lock.Unlock()
	if hc.me != "" {
		hc.log.Info("Client already started", "me", hc.me)
		return
	}
	hc.me = hc.mc.Id()

	hc.from, err = hc.mc.Subscribe(ctx, ClientTopic(hc.mc.Id()), 8, InstanceName+"/client")
	if err != nil {
		hc.log.Error(err, "Failed to subscribe to client topic", "topic", ClientTopic(hc.mc.Id()))
		return
	}
	// Note: Subscriber() waits for MQTT subscription ACK via token.WaitTimeout()
	// so the subscription is guaranteed to be active when it returns successfully

	hc.to, err = hc.mc.Publisher(ctx, ServerTopic(), 8, mqtt.AtLeastOnce, false, InstanceName+"/client")
	if err != nil {
		hc.log.Error(err, "Failed to prepare publishing to server topic", "topic", ServerTopic())
		return
	}

	hc.log.Info("Started client", "me", hc.mc.Id())
}

// func (hc *client) Shutdown() {
// 	hc.lock.Lock()
// 	defer hc.lock.Unlock()
// 	if hc.me == "" {
// 		hc.log.Info("Client not started")
// 		return
// 	}
// 	hc.log.Info("Shutting down client", "me", hc.me)
// 	hc.me = ""
// 	hc.from = nil
// 	hc.to = nil
// }

func (hc *client) LookupDevices(ctx context.Context, name string) (*[]devices.Device, error) {
	if strings.HasSuffix(name, ".local") {
		ips, err := mynet.MyResolver(hc.log).LookupHost(ctx, strings.TrimSuffix(name, ".local"))
		if err != nil {
			return nil, err
		}
		name = ips[0].String()
	}

	ip := net.ParseIP(name)
	if ip != nil {
		device, err := shelly.NewDeviceFromIp(ctx, hc.log, ip)
		if err != nil {
			return nil, err
		}
		return &[]devices.Device{device}, nil
	}

	hc.start(ctx)

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
	hc.start(ctx)

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
	hc.start(ctx)

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
			Dst: InstanceName,
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

	hc.log.Info("Calling method", "method", req.Method, "params", req.Params, "request_id", requestId, "dst", req.Dst)
	hc.to <- reqStr
	hc.log.Info("Request published", "topic", ServerTopic(), "request_id", requestId)

	var resStr []byte
	select {
	case <-ctx.Done():
		// Don't log context cancellation as an error
		return nil, ctx.Err()
	case resStr = <-hc.from:
		hc.log.Info("Response received", "payload", string(resStr), "request_id", requestId)
		break
	case <-time.After(hc.timeout):
		return nil, fmt.Errorf("timeout waiting for response to method %s after %v (request_id: %s, dst: %s, topic: %s)",
			method, hc.timeout, requestId, req.Dst, ClientTopic(hc.me))
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
