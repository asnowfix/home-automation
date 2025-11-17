package script

import (
	"context"

	"encoding/json"

	"github.com/dop251/goja"
	"github.com/go-logr/logr"
)

// each call to Shelly.addEventHandler creates an eventHandler
type eventHandler struct {
	callback goja.Callable
	userdata goja.Value
}

// there is one eventsHandler per script
type eventsHandler struct {
	log        logr.Logger
	eventChan  chan goja.Value
	handlers   []eventHandler
	vm         *goja.Runtime
	signalChan chan []byte
	id         int
}

func NewEventsHandler(ctx context.Context, vm *goja.Runtime) *eventsHandler {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic(err)
	}
	return &eventsHandler{
		log:        log.WithValues("eventsHandler", "eventsHandler"),
		eventChan:  make(chan goja.Value, 100),
		vm:         vm,
		handlers:   make([]eventHandler, 0),
		signalChan: nil,
		id:         0,
	}
}

func (eh *eventsHandler) NextId() int {
	eh.id++
	return eh.id
}

func (eh *eventsHandler) AddHandler(callback goja.Callable, userdata goja.Value) {
	eh.handlers = append(eh.handlers, eventHandler{
		callback: callback,
		userdata: userdata,
	})
}

func (eh *eventsHandler) Broadcaster() chan<- goja.Value {
	return eh.eventChan
}

func (eh *eventsHandler) Wait() <-chan []byte {
	// Create signal channel if not exists
	if eh.signalChan == nil {
		eh.log.Info("eventsHandler.Wait() called - starting goroutine")
		eh.signalChan = make(chan []byte)
		go func() {
			eh.log.Info("eventsHandler goroutine started, waiting for events...")
			for eventData := range eh.eventChan {
				data := eventData.Export()
				eh.log.Info("Event received from eventChan", "data", data)
				if data != nil {
					dataBytes, err := json.Marshal(data)
					if err != nil {
						eh.log.Error(err, "Failed to marshal event", "data", data)
						continue
					}
					// Forward event once - Handle() will call all handlers
					eh.log.Info("Forwarding event to signalChan", "dataBytes", string(dataBytes))
					eh.signalChan <- dataBytes
					eh.log.Info("Event forwarded to signalChan")
				}
			}
			eh.log.Info("eventChan closed, closing signalChan")
			close(eh.signalChan)
		}()
	}
	eh.log.Info("eventsHandler.Wait() returning signalChan")
	return eh.signalChan
}

func (eh *eventsHandler) Handle(ctx context.Context, vm *goja.Runtime, msg []byte) error {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	// msg already contains the event data (consumed by event loop's reflect.Select)
	eh.log.Info("Handle() called with event", "msg", string(msg))

	// Unmarshal the event data
	var eventData map[string]interface{}
	if err := json.Unmarshal(msg, &eventData); err != nil {
		log.Error(err, "Failed to unmarshal event", "msg", string(msg))
		return err
	}

	// Convert Go map to goja.Value
	eventObj := vm.ToValue(eventData)

	log.Info("Processing event", "event", eventData)

	// Call all registered event handlers
	for i, handler := range eh.handlers {
		eh.log.Info("Calling event handler", "handler", i, "event", eventData)
		_, err := handler.callback(goja.Undefined(), eventObj, handler.userdata)
		if err != nil {
			log.Error(err, "Event handler failed", "handler", i, "event", eventData)
		}
	}

	return nil
}
