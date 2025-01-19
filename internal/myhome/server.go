package myhome

import (
	"context"
	"encoding/json"
	"mymqtt"
	"reflect"

	"github.com/go-logr/logr"
)

type serverProxy struct {
	mc      *mymqtt.Client
	handler Handler
	cancel  context.CancelFunc
	from    <-chan []byte
}

type Handler interface {
	CallE(method string, params any) (any, error)
	InType() reflect.Type
	OutType() reflect.Type
}

func NewServerProxyE(ctx context.Context, log logr.Logger, mc *mymqtt.Client, handler Handler) (Proxy, error) {
	sctx, cancel := context.WithCancel(ctx)
	from, err := mc.Subscriber(ctx, ServerTopic(), 1)
	if err != nil {
		cancel()
		return nil, err
	}
	go func(ctx context.Context, log logr.Logger) {
		for {
			select {
			case <-ctx.Done():
				log.Info("Cancelled")
				return
			case inMsg := <-from:
				log.Info("Received message", "payload", string(inMsg))
				var req request
				req.Params = reflect.New(handler.InType()).Elem()
				err := json.Unmarshal(inMsg, &req)
				if err != nil {
					log.Error(err, "Failed to unmarshal request from payload", "payload", string(inMsg))
					continue
				}

				if err := ValidateDialog(req.Dialog); err != nil {
					log.Error(err, "invalid dialog")
					panic(err)
					// continue
				}
				res := &response{
					Dialog: Dialog{
						Id:  req.Id,
						Src: mc.Id(),
						Dst: req.Src,
					},
					Result: reflect.New(handler.OutType()).Elem(),
				}

				out, err := handler.CallE(req.Method, req.Params)
				if err != nil {
					log.Error(err, "Failed to call handler")
					res.Error = &Error{Code: 1, Message: err.Error()}
					res.Result = nil
				} else {
					res.Result = out
				}
				outMsg, err := json.Marshal(res)
				if err != nil {
					log.Error(err, "Failed to marshal response")
					res.Error = &Error{Code: 1, Message: err.Error()}
					res.Result = nil
					outMsg, _ = json.Marshal(res)
				}
				mc.Publish(ClientTopic(req.Src), outMsg)
			}
		}
	}(sctx, log.WithName("Server#Subscriber"))

	log.Info("Server running")
	return &serverProxy{
		mc:      mc,
		handler: handler,
		cancel:  cancel,
		from:    from,
	}, nil
}

func (sp *serverProxy) CallE(method string, params any) (any, error) {
	return sp.handler.CallE(method, params)
}

func (sp *serverProxy) Shutdown() {
	if sp.cancel != nil {
		sp.cancel()
		sp.cancel = nil
	}
}
