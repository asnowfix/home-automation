package myhome

import (
	"context"
	"encoding/json"
	"fmt"
	"mymqtt"
	"reflect"

	"github.com/go-logr/logr"
)

type server struct {
	mc      *mymqtt.Client
	handler Server
	cancel  context.CancelFunc
	from    <-chan []byte
}

type Server interface {
	MethodE(method string) (*Method, error)
	Shutdown()
}

func NewServerE(ctx context.Context, log logr.Logger, mc *mymqtt.Client, handler Server) (Server, error) {
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
				var res response
				var err error

				err = json.Unmarshal(inMsg, &req)
				if err != nil {
					log.Error(err, "Failed to unmarshal request from payload", "payload", string(inMsg))
					fail(1, err, &req, mc)
					continue
				}

				err = ValidateDialog(req.Dialog)
				if err != nil {
					fail(1, err, &req, mc)
					continue
				}

				method, err := handler.MethodE(req.Method)
				if err != nil {
					log.Error(err, "Failed to get action for method", "method", req.Method)
					fail(1, err, &req, mc)
					continue
				}

				// re-do Unmarshalling with proper types in place, if needed
				if method.Signature.NewParams != nil {
					req.Params = method.Signature.NewParams()
					err = json.Unmarshal(inMsg, &req)
					if err != nil {
						log.Error(err, "Failed to unmarshal request from payload", "payload", string(inMsg))
						fail(1, err, &req, mc)
						continue
					}
				}

				if method.Signature.NewResult != nil {
					res.Result = method.Signature.NewResult()
				}

				res.Dialog = Dialog{
					Id:  req.Id,
					Src: mc.Id(),
					Dst: req.Src,
				}

				out, err := method.ActionE(req.Params)
				if err != nil {
					log.Error(err, "Failed to call action")
					fail(1, err, &req, mc)
					continue
				}

				if reflect.TypeOf(out) != reflect.TypeOf(res.Result) {
					fail(1, fmt.Errorf("unexpected type returned from action: got %v, want %v", reflect.TypeOf(out), reflect.TypeOf(res.Result)), &req, mc)
					continue
				}

				res.Result = out

				outMsg, err := json.Marshal(res)
				if err != nil {
					log.Error(err, "Failed to marshal response")
					fail(1, err, &req, mc)
					continue
				}
				mc.Publish(ClientTopic(req.Src), outMsg)
			}
		}
	}(sctx, log.WithName("Server#Subscriber"))

	log.Info("Server running")
	return &server{
		mc:      mc,
		handler: handler,
		cancel:  cancel,
		from:    from,
	}, nil
}

func fail(code int, err error, req *request, mc *mymqtt.Client) {
	var res response = response{
		Dialog: Dialog{
			Id:  req.Id,
			Src: mc.Id(),
			Dst: req.Src,
		},
		Error:  &Error{Code: code, Message: err.Error()},
		Result: nil,
	}
	outMsg, _ := json.Marshal(res)
	mc.Publish(ClientTopic(res.Dst), outMsg)
}

func (sp *server) MethodE(method string) (*Method, error) {
	return sp.handler.MethodE(method)
}

func (sp *server) Shutdown() {
	if sp.cancel != nil {
		sp.cancel()
		sp.cancel = nil
	}
}
