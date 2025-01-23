package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"mymqtt"
	"reflect"

	"github.com/go-logr/logr"
)

type client struct {
	log    logr.Logger
	to     chan<- []byte
	from   <-chan []byte
	cancel context.CancelFunc
	me     string
}

func NewClientE(ctx context.Context, log logr.Logger, mc *mymqtt.Client) (Client, error) {
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

	return &client{
		log:    log,
		from:   from,
		to:     to,
		cancel: cancel,
		me:     mc.Id(),
	}, nil
}

func (hc *client) Shutdown() {
	if hc.cancel != nil {
		hc.cancel()
	}
}

func (hc *client) CallE(method string, params any) (any, error) {
	requestId, err := RandStringBytesMaskImprRandReaderUnsafe(16)
	if err != nil {
		return nil, err
	}

	m, exists := signatures[method]
	if !exists {
		return Method{}, fmt.Errorf("unknown method %s", method)
	}

	if reflect.TypeOf(params) != reflect.TypeOf(m.NewParams()) {
		return nil, fmt.Errorf("invalid parameters for method %s: got %v", method, reflect.TypeOf(params))
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
