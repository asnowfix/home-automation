package myhome

import (
	"context"
	"encoding/json"

	"github.com/asnowfix/home-automation/myhome/mqtt"

	"github.com/go-logr/logr"
)

// maxConcurrentRPCs bounds how many requests the server processes at once.
// Without a bound, a burst of requests would spawn unbounded goroutines; with
// no dispatch at all (the old behavior), one slow handler head-of-line-blocks
// every other RPC, including the UI. See TestServer_SlowHandlerDoesNotBlockConcurrentRPC.
const maxConcurrentRPCs = 16

type server struct {
	handler Server
	from    <-chan []byte
}

type Server interface {
	MethodE(method Verb) (*Method, error)
}

func NewServerE(ctx context.Context, mc mqtt.Client, handler Server) (Server, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		// A missing logger in ctx is not fatal: fall back to a discard logger
		// rather than panicking the caller's goroutine.
		log = logr.Discard()
	}
	log = log.WithName("myhome.rpc")
	from, err := mc.Subscribe(ctx, ServerTopic(), 16, InstanceName+"/server")
	if err != nil {
		log.Error(err, "Failed to subscribe to server", "topic", ServerTopic())
		return nil, err
	}
	s := &server{
		handler: handler,
		from:    from,
	}

	go s.loop(logr.NewContext(ctx, log.WithName("Server")), mc)

	log.Info("Server started")
	return s, nil
}

// loop is the single reader of s.from. It bounds concurrency with a
// semaphore of size maxConcurrentRPCs and hands each message to its own
// goroutine, so one slow handler cannot delay any other in-flight request.
func (sp *server) loop(ctx context.Context, mc mqtt.Client) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		log = logr.Discard()
	}
	log.Info("Server message loop started")

	sem := make(chan struct{}, maxConcurrentRPCs)
	for {
		select {
		case <-ctx.Done():
			log.Info("Cancelled", "reason", ctx.Err())
			return
		case inMsg, ok := <-sp.from:
			if !ok {
				log.Info("Server subscription channel closed")
				return
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				log.Info("Cancelled while waiting for a dispatch slot", "reason", ctx.Err())
				return
			}
			go func(inMsg []byte) {
				defer func() { <-sem }()
				sp.handle(ctx, log, mc, inMsg)
			}(inMsg)
		}
	}
}

// handle decodes and dispatches a single request message, publishing exactly
// one response (success or failure) back to the requester.
func (sp *server) handle(ctx context.Context, log logr.Logger, mc mqtt.Client, inMsg []byte) {
	log.Info("Received message", "payload", string(inMsg))

	var req request
	if err := json.Unmarshal(inMsg, &req); err != nil {
		log.Error(err, "Failed to unmarshal request from payload", "payload", string(inMsg))
		sp.fail(ctx, log, 1, err, &req, mc)
		return
	}

	if err := ValidateDialog(req.Dialog); err != nil {
		log.Error(err, "Invalid dialog:"+req.String())
		sp.fail(ctx, log, 1, err, &req, mc)
		return
	}

	method, err := sp.handler.MethodE(req.Method)
	if err != nil {
		log.Error(err, "Failed to get action for method", "method", req.Method)
		sp.fail(ctx, log, 1, err, &req, mc)
		return
	}

	out, err := method.Action(ctx, req.Params)
	if err != nil {
		log.Error(err, "Failed to call action")
		sp.fail(ctx, log, 1, err, &req, mc)
		return
	}

	resultRaw, err := json.Marshal(out)
	if err != nil {
		log.Error(err, "Failed to marshal result")
		sp.fail(ctx, log, 1, err, &req, mc)
		return
	}

	res := response{
		Dialog: Dialog{
			Id:  req.Id,
			Src: mc.Id(),
			Dst: req.Src,
		},
		Result: resultRaw,
	}

	outMsg, err := json.Marshal(res)
	if err != nil {
		log.Error(err, "Failed to marshal response")
		sp.fail(ctx, log, 1, err, &req, mc)
		return
	}
	log.Info("Publishing response", "dst", res.Dst, "topic", ClientTopic(req.Src), "request_id", res.Id)
	if err := mc.Publish(ctx, ClientTopic(req.Src), outMsg, mqtt.AtLeastOnce, false, InstanceName+".rpc/Server"); err != nil {
		log.Error(err, "Failed to publish response", "dst", res.Dst, "request_id", res.Id)
	}
}

func (sp *server) fail(ctx context.Context, log logr.Logger, code int, err error, req *request, mc mqtt.Client) {
	res := response{
		Dialog: Dialog{
			Id:  req.Id,
			Src: mc.Id(),
			Dst: req.Src,
		},
		Error: &Error{Code: code, Message: err.Error()},
	}
	outMsg, merr := json.Marshal(res)
	if merr != nil {
		// response carries only strings, ints and raw bytes; this cannot happen,
		// but don't silently swallow it if it ever does.
		log.Error(merr, "Failed to marshal error response", "dst", res.Dst, "request_id", res.Id)
		return
	}
	// Best-effort: the requester will time out if this error response is lost.
	if perr := mc.Publish(ctx, ClientTopic(res.Dst), outMsg, mqtt.AtLeastOnce, false, InstanceName+".rpc/Server"); perr != nil {
		log.Error(perr, "Failed to publish error response", "dst", res.Dst, "request_id", res.Id)
	}
}

func (sp *server) MethodE(method Verb) (*Method, error) {
	return sp.handler.MethodE(method)
}
