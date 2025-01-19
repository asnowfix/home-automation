package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"mymqtt"
	"reflect"

	"github.com/go-logr/logr"
)

type clientProxy struct {
	to     chan<- []byte
	from   <-chan []byte
	cancel context.CancelFunc
	me     string
}

func NewClientProxyE(ctx context.Context, log logr.Logger, mc *mymqtt.Client) (Proxy, error) {
	hctx, cancel := context.WithCancel(ctx)
	from, err := mc.Subscriber(hctx, ClientTopic(mc.Id()), 1)
	if err != nil {
		cancel()
		return nil, err
	}

	to, err := mc.Publisher(hctx, ServerTopic(), 1)
	if err != nil {
		cancel()
		return nil, err
	}

	return &clientProxy{
		from:   from,
		to:     to,
		cancel: cancel,
		me:     mc.Id(),
	}, nil
}

func (hc *clientProxy) Shutdown() {
	if hc.cancel != nil {
		hc.cancel()
	}
}

func (hc *clientProxy) CallE(method string, params any) (any, error) {
	requestId, err := RandStringBytesMaskImprRandReaderUnsafe(16)
	if err != nil {
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

	hc.to <- reqStr
	resStr := <-hc.from
	var res response
	res.Result = reflect.New(handler.OutType()).Elem()
	err = json.Unmarshal(resStr, &res)
	if err != nil {
		return nil, err
	}

	if err := ValidateDialog(res.Dialog); err != nil {
		return nil, err
	}

	if res.Error != nil {
		return nil, fmt.Errorf("%v (code:%v)", res.Error.Message, res.Error.Code)
	}

	return res.Result, nil
}
