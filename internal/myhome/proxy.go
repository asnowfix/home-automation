package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"mymqtt"
	"os"

	"github.com/go-logr/logr"
)

type Proxy interface {
	CallE(method string, in any) (any, error)
	Shutdown()
}

type clientProxy struct {
	me     string
	to     chan<- []byte
	from   <-chan []byte
	cancel context.CancelFunc
}

func NewClientProxyE(ctx context.Context, log logr.Logger, broker string) (Proxy, error) {
	mc, err := mymqtt.NewClientE(log, broker)
	if err != nil {
		return nil, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	me := fmt.Sprintf("%s_%d", hostname, os.Getpid())

	hctx, cancel := context.WithCancel(ctx)
	from, err := mc.Subscriber(hctx, fmt.Sprintf("%s/%s/rpc", me, MYHOME), 1)
	if err != nil {
		cancel()
		return nil, err
	}

	to, err := mc.Publisher(hctx, fmt.Sprintf("%s/rpc", MYHOME), 1)
	if err != nil {
		cancel()
		return nil, err
	}

	return &clientProxy{
		me:     me,
		from:   from,
		to:     to,
		cancel: cancel,
	}, nil
}

func (hc *clientProxy) Shutdown() {
	if hc.cancel != nil {
		hc.cancel()
	}
}

type request struct {
	RequestId string `json:"request_id"`
	Src       string `json:"src"`
	Dst       string `json:"dst"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
}

type response struct {
	RequestId string `json:"request_id"`
	Src       string `json:"src"`
	Dst       string `json:"dst"`
	Msg       any    `json:"msg"`
}

func (h *clientProxy) CallE(method string, in any) (any, error) {
	requestId, err := RandStringBytesMaskImprRandReaderUnsafe(16)
	if err != nil {
		return nil, err
	}
	req := request{
		RequestId: requestId,
		Src:       h.me,
		Dst:       MYHOME,
		Method:    method,
		Params:    in,
	}
	reqStr, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	h.to <- reqStr
	resStr := <-h.from
	var res response
	err = json.Unmarshal(resStr, &res)
	if err != nil {
		return nil, err
	}
	if res.RequestId != requestId {
		return nil, fmt.Errorf("invalid respons: request-id=%v, expected=%v", res.RequestId, requestId)
	}
	return res.Msg, nil
}
