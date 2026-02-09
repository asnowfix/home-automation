package myhome

import (
	"context"
	"encoding/json"
	"myhome/mqtt"

	"github.com/go-logr/logr"
)

type server struct {
	// mc      *mymqtt.Client
	handler Server
	from    <-chan []byte
	// to      chan []byte
}

type Server interface {
	MethodE(method Verb) (*Method, error)
}

func NewServerE(ctx context.Context, handler Server) (Server, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic(err)
	}
	log = log.WithName("myhome.rpc")
	mc, err := mqtt.GetClientE(ctx)
	if err != nil {
		return nil, err
	}
	from, err := mc.Subscribe(ctx, ServerTopic(), 16, InstanceName+"/server")
	if err != nil {
		log.Error(err, "Failed to subscribe to server", "topic", ServerTopic())
		return nil, err
	}
	// to, err := mc.Publisher(ctx, ServerTopic(), 1)
	// if err != nil {
	// 	log.Error(err, "Failed to publish to server", "topic", ServerTopic())
	// 	return nil, err
	// }
	s := server{
		// mc:      mc,
		handler: handler,
		from:    from,
		// to:      to,
	}

	go func(ctx context.Context) {
		log, err := logr.FromContext(ctx)
		if err != nil {
			panic(err)
		}
		log.Info("Server message loop started")
		for {
			select {
			case <-ctx.Done():
				log.Info("Cancelled", "reason", ctx.Err())
				return
			case inMsg := <-from:
				log.Info("Received message", "payload", string(inMsg))
				var req request
				var res response
				var err error

				err = json.Unmarshal(inMsg, &req)
				if err != nil {
					log.Error(err, "Failed to unmarshal request from payload", "payload", string(inMsg))
					s.fail(ctx, 1, err, &req, mc)
					continue
				}

				err = ValidateDialog(req.Dialog)
				if err != nil {
					log.Error(err, "Invalid dialog:"+req.Dialog.String())
					s.fail(ctx, 1, err, &req, mc)
					continue
				}

				method, err := handler.MethodE(req.Method)
				if err != nil {
					log.Error(err, "Failed to get action for method", "method", req.Method)
					s.fail(ctx, 1, err, &req, mc)
					continue
				}

				// re-do Unmarshalling with proper types in place, if needed
				req.Params = method.Signature.NewParams()
				err = json.Unmarshal(inMsg, &req)
				if err != nil {
					log.Error(err, "Failed to unmarshal request from payload", "payload", string(inMsg))
					s.fail(ctx, 1, err, &req, mc)
					continue
				}

				tempResult := method.Signature.NewResult()
				res.Result = &tempResult

				res.Dialog = Dialog{
					Id:  req.Id,
					Src: mc.Id(),
					Dst: req.Src,
				}

				out, err := method.ActionE(ctx, req.Params)
				if err != nil {
					log.Error(err, "Failed to call action")
					s.fail(ctx, 1, err, &req, mc)
					continue
				}

				// FIXME: produces errors like: `Error: unexpected type returned from action: got *[]devices.Device, want *interface {} (code:1)`
				// if reflect.TypeOf(out) != reflect.TypeOf(res.Result) {
				// 	s.fail(ctx, 1, fmt.Errorf("unexpected type returned from action: got %v, want %v", reflect.TypeOf(out), reflect.TypeOf(res.Result)), &req, mc)
				// 	continue
				// }

				res.Result = &out

				outMsg, err := json.Marshal(res)
				if err != nil {
					log.Error(err, "Failed to marshal response")
					s.fail(ctx, 1, err, &req, mc)
					continue
				}
				// to <- outMsg
				log.Info("Publishing response", "dst", res.Dst, "topic", ClientTopic(req.Src), "request_id", res.Id)
				mc.Publish(ctx, ClientTopic(req.Src), outMsg, mqtt.AtLeastOnce, false, InstanceName+".rpc/Server")
			}
		}
	}(logr.NewContext(ctx, log.WithName("Server")))

	log.Info("Server started")
	return &s, nil
}

func (sp *server) fail(ctx context.Context, code int, err error, req *request, mc mqtt.Client) {
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
	// sp.to <- outMsg
	mc.Publish(ctx, ClientTopic(res.Dst), outMsg, mqtt.AtLeastOnce, false, InstanceName+".rpc/Server")
}

func (sp *server) MethodE(method Verb) (*Method, error) {
	return sp.handler.MethodE(method)
}
