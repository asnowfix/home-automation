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
		from:   from,
		to:     to,
		cancel: cancel,
		me:     mc.Id(),
	}, nil
}

func (c *client) Shutdown() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (hc *client) CallE(method string, params any) (any, error) {
	requestId, err := RandStringBytesMaskImprRandReaderUnsafe(16)
	if err != nil {
		return nil, err
	}

	m, exists := Methods[method]
	if !exists {
		return Method{}, fmt.Errorf("unknown method %s", method)
	}

	if reflect.TypeOf(params) != m.InType {
		return nil, fmt.Errorf("invalid parameters for method %s: got %v, want %v", method, reflect.TypeOf(params), m.InType)
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
	res.Result = reflect.New(m.OutType).Elem()
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

// func (hc *client) MethodE(method string) (Method, error) {
// 	m, exists := Methods[method]
// 	if !exists {
// 		return Method{}, fmt.Errorf("unknown method %s", method)
// 	}
// 	return Method{
// 		InType:  m.InType,
// 		OutType: m.OutType,
// 		ActionE: nil,
// 	}, nil
// }
