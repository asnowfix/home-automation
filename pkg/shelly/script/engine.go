package script

import (
	"context"
	"fmt"

	"github.com/asnowfix/home-automation/pkg/shelly/mqtt"

	"github.com/dop251/goja"
	"github.com/go-logr/logr"
)

// EngineOptions configures a script Engine beyond the plain device emulation
// provided by Run().
type EngineOptions struct {
	// State is the persistent KVS/Script.storage backing store. When nil, an
	// empty in-memory state is used.
	State *DeviceState

	// Customize, when non-nil, is called after the Shelly runtime globals are
	// installed and before the script is evaluated. It lets hosts add extra
	// globals (e.g. the daemon's MyHome API).
	Customize func(vm *goja.Runtime) error

	// EnableExternalCalls keeps the event loop alive even when the script
	// registers no timer/MQTT handlers, so that Dispatch() can be served.
	// Daemon-hosted scripts set this; the CLI emulator does not.
	EnableExternalCalls bool

	// TODO(#250): add a Mode field (DeviceTestMode | DeviceExtensionMode) once
	// resource-limit emulation lands. myhome/scripthost must construct its
	// engines with DeviceExtensionMode (unlimited) — daemon-hosted scripts
	// exist precisely to subcontract work that exceeds real device limits.
	// Device-bound script tests must default to DeviceTestMode (enforced).
}

// Engine runs one script in one goja VM. The VM is single-threaded: the
// goroutine that calls Start() owns it, and all external entry points must go
// through the event loop via Dispatch()/DispatchAsync().
type Engine struct {
	name     string
	source   []byte
	vm       *goja.Runtime
	handlers []handler
	state    *DeviceState
	external *externalHandler
	log      logr.Logger
}

// NewEngine builds the goja VM with the Shelly runtime globals (Timer, MQTT,
// Shelly, Script.storage, KVS-backed Shelly.call methods…) but does not
// evaluate the script yet: Start() does, on the goroutine that will own the VM.
func NewEngine(ctx context.Context, name string, source []byte, opts EngineOptions) (*Engine, error) {
	log, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	log = log.WithName("script.engine").WithValues("script", name)

	mc := mqtt.GetClient(ctx)
	if mc == nil {
		return nil, fmt.Errorf("MQTT client not initialized")
	}

	state := opts.State
	if state == nil {
		state = &DeviceState{
			KVS:     make(map[string]interface{}),
			Storage: make(map[string]interface{}),
		}
	}

	e := &Engine{
		name:     name,
		source:   source,
		state:    state,
		handlers: make([]handler, 0),
		log:      log,
	}

	vm, err := createShellyRuntime(ctx, mc, &e.handlers, state)
	if err != nil {
		return nil, err
	}
	e.vm = vm

	if opts.Customize != nil {
		if err := opts.Customize(vm); err != nil {
			return nil, fmt.Errorf("customize %s: %w", name, err)
		}
	}

	if opts.EnableExternalCalls {
		e.external = newExternalHandler()
		e.handlers = append(e.handlers, e.external)
	}

	return e, nil
}

// Name returns the script name this engine runs.
func (e *Engine) Name() string { return e.name }

// State returns the engine's backing device state (KVS + Script.storage).
func (e *Engine) State() *DeviceState { return e.state }

// Start evaluates the script then runs the event loop until the context is
// cancelled (or, without EnableExternalCalls, until all handlers are closed).
// It blocks: run it on a dedicated goroutine, which becomes the VM owner.
func (e *Engine) Start(ctx context.Context) error {
	out, err := e.vm.RunScript(e.name, string(e.source))
	if err != nil {
		e.log.Error(err, "Script evaluation failed")
		return err
	}
	e.log.Info("Script evaluated", "out", out)

	if len(e.handlers) == 0 {
		e.log.Info("No handlers registered, exiting")
		return nil
	}

	return runEventLoop(logr.NewContext(ctx, e.log), e.vm, &e.handlers)
}

// Dispatch runs fn on the VM goroutine and waits for it to complete. It is
// the only safe way to touch the VM from outside the event loop. Never call
// it from script code (i.e. from within the VM goroutine): that deadlocks.
func (e *Engine) Dispatch(ctx context.Context, fn func(vm *goja.Runtime)) error {
	if e.external == nil {
		return fmt.Errorf("engine %s was created without EnableExternalCalls", e.name)
	}
	call := &externalCall{fn: fn, done: make(chan struct{})}
	if err := e.external.enqueue(ctx, call); err != nil {
		return err
	}
	select {
	case <-call.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DispatchAsync queues fn for execution on the VM goroutine without waiting.
// Safe to call from anywhere, including goroutines spawned by script APIs.
func (e *Engine) DispatchAsync(ctx context.Context, fn func(vm *goja.Runtime)) error {
	if e.external == nil {
		return fmt.Errorf("engine %s was created without EnableExternalCalls", e.name)
	}
	return e.external.enqueue(ctx, &externalCall{fn: fn})
}

// externalCall is a function to execute on the VM goroutine. done, when
// non-nil, is closed after execution.
type externalCall struct {
	fn   func(vm *goja.Runtime)
	done chan struct{}
}

// externalHandler funnels externally-submitted calls into the event loop. It
// satisfies the handler interface: each queued call is signalled on the Wait()
// channel and executed by Handle() on the VM goroutine.
type externalHandler struct {
	signal chan []byte
	queue  chan *externalCall
}

func newExternalHandler() *externalHandler {
	return &externalHandler{
		signal: make(chan []byte, 64),
		queue:  make(chan *externalCall, 64),
	}
}

func (h *externalHandler) enqueue(ctx context.Context, call *externalCall) error {
	select {
	case h.queue <- call:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case h.signal <- []byte{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (h *externalHandler) Wait() <-chan []byte { return h.signal }

func (h *externalHandler) Handle(ctx context.Context, vm *goja.Runtime, msg []byte) error {
	select {
	case call := <-h.queue:
		call.fn(vm)
		if call.done != nil {
			close(call.done)
		}
	default:
		// Signal without a queued call (should not happen): ignore.
	}
	return nil
}
