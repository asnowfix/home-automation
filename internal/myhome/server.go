package myhome

import (
	"context"
	"encoding/json"
	"mymqtt"

	"github.com/go-logr/logr"
)

type server struct {
	// mc      *mymqtt.Client
	handler Server
	from    chan []byte
	// to      chan []byte
}

type Server interface {
	MethodE(method Verb) (*Method, error)
}

func NewServerE(ctx context.Context, log logr.Logger, mc *mymqtt.Client, handler Server) (Server, error) {
	from, err := mc.Subscriber(ctx, ServerTopic(), 1)
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

	go func(ctx context.Context, log logr.Logger) {
		for {
			log.Info("Waiting for message")
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
					s.fail(1, err, &req, mc)
					continue
				}

				err = ValidateDialog(req.Dialog)
				if err != nil {
					s.fail(1, err, &req, mc)
					continue
				}

				method, err := handler.MethodE(req.Method)
				if err != nil {
					log.Error(err, "Failed to get action for method", "method", req.Method)
					s.fail(1, err, &req, mc)
					continue
				}

				// re-do Unmarshalling with proper types in place, if needed
				req.Params = method.Signature.NewParams()
				err = json.Unmarshal(inMsg, &req)
				if err != nil {
					log.Error(err, "Failed to unmarshal request from payload", "payload", string(inMsg))
					s.fail(1, err, &req, mc)
					continue
				}

				tempResult := method.Signature.NewResult()
				res.Result = &tempResult

				res.Dialog = Dialog{
					Id:  req.Id,
					Src: mc.Id(),
					Dst: req.Src,
				}

				out, err := method.ActionE(req.Params)
				if err != nil {
					log.Error(err, "Failed to call action")
					s.fail(1, err, &req, mc)
					continue
				}

				// if reflect.TypeOf(out) != reflect.TypeOf(res.Result) {
				// 	fail(1, fmt.Errorf("unexpected type returned from action: got %v, want %v", reflect.TypeOf(out), reflect.TypeOf(res.Result)), &req, mc)
				// 	continue
				// }

				res.Result = &out

				outMsg, err := json.Marshal(res)
				if err != nil {
					log.Error(err, "Failed to marshal response")
					s.fail(1, err, &req, mc)
					continue
				}
				// to <- outMsg
				mc.Publish(ClientTopic(req.Src), outMsg)
			}
		}
	}(ctx, log.WithName("Server#Subscriber"))

	log.Info("Server running")
	return &s, nil
}

func (sp *server) fail(code int, err error, req *request, mc *mymqtt.Client) {
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
	mc.Publish(ClientTopic(res.Dst), outMsg)
}

func (sp *server) MethodE(method Verb) (*Method, error) {
	return sp.handler.MethodE(method)
}
