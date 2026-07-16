package myhome

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"

	mynet "github.com/asnowfix/home-automation/internal/myhome/net"
	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly"

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

	// pendingLock guards pending, the routing table used to correlate
	// in-flight requests with their responses (see dispatch/CallE).
	pendingLock sync.Mutex
	pending     map[string]chan response
}

func NewClientE(ctx context.Context, log logr.Logger, mc mqtt.Client, timeout time.Duration) (Client, error) {
	c := &client{
		log:     log,
		timeout: timeout,
		mc:      mc,
	}
	return c, nil
}

// newRequestID returns a random hex-encoded request identifier suitable for
// Dialog.Id. It uses crypto/rand directly rather than a bespoke
// charset-mapping helper.
func newRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := crand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// start lazily connects the client to the RPC transport: it subscribes to
// this client's response topic and prepares a publisher for the server's
// request topic. It is safe to call concurrently and idempotent once it has
// succeeded.
//
// On failure hc.me is left unset, so a later call retries the connection
// from scratch instead of getting stuck: previously hc.me was set before the
// subscribe/publish calls, so a failed start() silently left hc.to nil and
// every subsequent CallE blocked forever sending on it.
func (hc *client) start(ctx context.Context) error {
	hc.lock.Lock()
	defer hc.lock.Unlock()
	if hc.me != "" {
		return nil
	}

	me := hc.mc.Id()

	from, err := hc.mc.Subscribe(ctx, ClientTopic(me), 8, InstanceName+"/client")
	if err != nil {
		hc.log.Error(err, "Failed to subscribe to client topic", "topic", ClientTopic(me))
		return fmt.Errorf("subscribe to client topic %s: %w", ClientTopic(me), err)
	}
	// Note: Subscriber() waits for MQTT subscription ACK via token.WaitTimeout()
	// so the subscription is guaranteed to be active when it returns successfully

	to, err := hc.mc.Publisher(ctx, ServerTopic(), 8, mqtt.AtLeastOnce, false, InstanceName+"/client")
	if err != nil {
		hc.log.Error(err, "Failed to prepare publishing to server topic", "topic", ServerTopic())
		return fmt.Errorf("prepare publisher for server topic %s: %w", ServerTopic(), err)
	}

	hc.me = me
	hc.from = from
	hc.to = to
	hc.pending = make(map[string]chan response)

	go hc.dispatch(from)

	hc.log.Info("Started client", "me", me)
	return nil
}

// dispatch is the single reader of hc.from. It runs for the lifetime of the
// client and routes every incoming response to the pending CallE waiting on
// it, keyed by Dialog.Id. This is what makes concurrent CallE calls safe:
// without it, whichever goroutine happened to read hc.from next could
// receive a response meant for a different caller.
func (hc *client) dispatch(from <-chan []byte) {
	for resStr := range from {
		var res response
		if err := json.Unmarshal(resStr, &res); err != nil {
			hc.log.Error(err, "Failed to unmarshal response envelope", "payload", string(resStr))
			continue
		}

		hc.pendingLock.Lock()
		ch, ok := hc.pending[res.Id]
		if ok {
			delete(hc.pending, res.Id)
		}
		hc.pendingLock.Unlock()

		if !ok {
			hc.log.Info("Discarding response with no matching pending request", "request_id", res.Id)
			continue
		}
		ch <- res
	}
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
		ips, err := mynet.MyResolver(hc.log).LookupHost(ctx, hc.log, strings.TrimSuffix(name, ".local"))
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

	var out any
	var err error

	if strings.HasPrefix(name, "*") || strings.HasSuffix(name, "*") {
		out, err = hc.CallE(ctx, DevicesMatch, name)
	} else {
		out, err = hc.CallE(ctx, DeviceLookup, name)
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
	devices, err := hc.LookupDevices(ctx, name)
	if err != nil {
		return err
	}

	for _, d := range *devices {
		_, err = hc.CallE(ctx, DeviceForget, d.Id())
		if err != nil {
			return err
		}
	}
	return nil
}

func (hc *client) CallE(ctx context.Context, method Verb, params any) (any, error) {
	if err := hc.start(ctx); err != nil {
		return nil, err
	}

	requestId, err := newRequestID()
	if err != nil {
		return nil, err
	}

	m, exists := signatures[method]
	if !exists {
		return nil, fmt.Errorf("unknown method %s", method)
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

	// Register this call's response channel before publishing the request,
	// so dispatch can never race ahead of the registration.
	respCh := make(chan response, 1)
	hc.pendingLock.Lock()
	hc.pending[requestId] = respCh
	hc.pendingLock.Unlock()
	defer func() {
		hc.pendingLock.Lock()
		delete(hc.pending, requestId)
		hc.pendingLock.Unlock()
	}()

	hc.log.Info("Calling method", "method", req.Method, "params", req.Params, "request_id", requestId, "dst", req.Dst)
	select {
	case hc.to <- reqStr:
		hc.log.Info("Request published", "topic", ServerTopic(), "request_id", requestId)
	case <-ctx.Done():
		// Don't log context cancellation as an error
		return nil, ctx.Err()
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, hc.timeout)
	defer cancel()

	var res response
	select {
	case <-timeoutCtx.Done():
		if ctx.Err() != nil {
			// Don't log context cancellation as an error
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("timeout waiting for response to method %s after %v (request_id: %s, dst: %s, topic: %s)",
			method, hc.timeout, requestId, req.Dst, ClientTopic(hc.me))
	case res = <-respCh:
		hc.log.Info("Response received", "dialog", res.String(), "request_id", requestId)
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
