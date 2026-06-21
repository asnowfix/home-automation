// Package scripthost runs MyHome workflow scripts (JavaScript) on the daemon,
// using the same goja-based Shelly runtime as the CLI emulator
// (pkg/shelly/script). Workflows are written in JS; communication,
// persistence and infrastructure stay in Go and are exposed to scripts
// through the MyHome global (see jsapi.go).
//
// Scripts are resolved by name: an optional user directory first, then the
// embedded device-script library — so every device script can assume a script
// of the same name is also present on the daemon. Devices invoke daemon
// scripts through the script.invoke RPC verb on the regular myhome/rpc topic.
//
// Daemon-hosted scripts deliberately run unconstrained by Shelly device
// resource limits (5 timers, 5 event/status handlers, 10 MQTT subscriptions,
// ~30KB heap — see AGENTS.md "Resource Limits"): the whole point of hosting a
// workflow here instead of on the device is to subcontract memory/CPU-heavy
// work the device can't do. When script.Engine gains resource-limit
// emulation (issue #250), engines built by this package must use
// script.DeviceExtensionMode, never script.DeviceTestMode.
package scripthost

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/script"

	"github.com/dop251/goja"
	"github.com/go-logr/logr"
)

// Config configures the daemon script host (config keys daemon.scripts.*).
type Config struct {
	Enabled  bool     // daemon.scripts.enabled
	Dir      string   // daemon.scripts.dir — optional user scripts dir, overrides embedded scripts
	Run      []string // daemon.scripts.run — script names to run (with or without .js)
	StateDir string   // daemon.scripts.state_dir — per-script KVS/storage JSON files (default "scripts-state")
}

// DeviceProvider gives scripts access to managed devices (implemented by
// devices/impl.DeviceManager). Same contract as the Go heater service.
type DeviceProvider interface {
	GetDeviceByAny(ctx context.Context, identifier string) (*myhome.Device, error)
	GetShellyDevice(ctx context.Context, device *myhome.Device) (*shelly.Device, error)
}

// Service hosts one engine per configured script and serves script.invoke.
type Service struct {
	log      logr.Logger
	cfg      Config
	embedded fs.FS
	provider DeviceProvider
	instance string

	mu      sync.RWMutex
	runners map[string]*runner // key: script name without .js

	// deviceCall / uploadScript back MyHome.deviceCall and
	// MyHome.uploadScript; overridable for tests (see WithDeviceCaller).
	deviceCall   func(ctx context.Context, identifier, method string, params any) (any, error)
	uploadScript func(ctx context.Context, identifier, scriptName string) error
}

func NewService(log logr.Logger, cfg Config, embedded fs.FS, provider DeviceProvider, instance string) *Service {
	if cfg.StateDir == "" {
		cfg.StateDir = "scripts-state"
	}
	s := &Service{
		log:      log.WithName("scripthost"),
		cfg:      cfg,
		embedded: embedded,
		provider: provider,
		instance: instance,
		runners:  make(map[string]*runner),
	}
	s.deviceCall = s.doDeviceCall
	s.uploadScript = s.doUploadScript
	return s
}

// WithDeviceCaller overrides the device RPC backends (tests).
func (s *Service) WithDeviceCaller(
	deviceCall func(ctx context.Context, identifier, method string, params any) (any, error),
	uploadScript func(ctx context.Context, identifier, scriptName string) error,
) *Service {
	if deviceCall != nil {
		s.deviceCall = deviceCall
	}
	if uploadScript != nil {
		s.uploadScript = uploadScript
	}
	return s
}

// Start launches every configured script and registers the script.invoke RPC
// handler. Non-blocking: each script runs on its own goroutine until ctx ends.
func (s *Service) Start(ctx context.Context) error {
	if err := os.MkdirAll(s.cfg.StateDir, 0o755); err != nil {
		return fmt.Errorf("scripts state dir %s: %w", s.cfg.StateDir, err)
	}

	for _, name := range s.cfg.Run {
		name = normalizeName(name)
		if _, dup := s.runners[name]; dup {
			s.log.Info("Ignoring duplicate script", "script", name)
			continue
		}
		r := &runner{
			name: name,
			svc:  s,
			log:  s.log.WithValues("script", name),
		}
		s.mu.Lock()
		s.runners[name] = r
		s.mu.Unlock()
		go r.run(ctx)
	}

	myhome.RegisterMethodHandler(myhome.ScriptInvoke, func(ctx context.Context, in any) (any, error) {
		params, ok := in.(*myhome.ScriptInvokeParams)
		if !ok {
			return nil, fmt.Errorf("unexpected param type: %T", in)
		}
		return s.Invoke(ctx, params.Script, params.Name, params.Params)
	})

	s.log.Info("Script host started", "scripts", s.cfg.Run, "state_dir", s.cfg.StateDir)
	return nil
}

// Runs reports whether the given script is configured to run on this host
// (used by the daemon to substitute Go workflows with their JS versions).
func (s *Service) Runs(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.runners[normalizeName(name)]
	return ok
}

// Invoke calls a handler registered with MyHome.on(name, fn) in the given
// script, passing params (generic JSON values) and returning the handler's
// return value. It is the backend of the script.invoke RPC verb.
func (s *Service) Invoke(ctx context.Context, scriptName, name string, params any) (*myhome.ScriptInvokeResult, error) {
	s.mu.RLock()
	r := s.runners[normalizeName(scriptName)]
	s.mu.RUnlock()
	if r == nil {
		return nil, fmt.Errorf("script %s is not hosted here (instance %s)", scriptName, s.instance)
	}

	eng := r.engine()
	if eng == nil {
		return nil, fmt.Errorf("script %s is not running", scriptName)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	out, err := r.callJSHandler(ctx, eng, params, fmt.Sprintf("handler %q (MyHome.on)", name), func() (goja.Callable, bool) {
		cb, ok := r.invokeHandlers[name]
		return cb, ok
	})
	if err != nil {
		return nil, err
	}
	return &myhome.ScriptInvokeResult{Result: out}, nil
}

// runner manages the lifecycle of one script: load, engine build, restart
// with backoff on crash.
type runner struct {
	name string
	svc  *Service
	log  logr.Logger

	mu  sync.RWMutex
	eng *script.Engine

	// Handler maps are written during script evaluation and read by
	// Dispatch callbacks: both happen on the VM goroutine, so no lock.
	invokeHandlers map[string]goja.Callable
	verbHandlers   map[myhome.Verb]goja.Callable
	// registeredVerbs survives engine restarts: re-registration only swaps
	// the JS callable, not the Go-side method handler.
	registeredVerbs map[myhome.Verb]bool
}

func (r *runner) engine() *script.Engine {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.eng
}

func (r *runner) run(ctx context.Context) {
	backoff := time.Second
	for {
		started := time.Now()
		err := r.runOnce(ctx)
		if ctx.Err() != nil {
			r.log.Info("Script stopped", "reason", ctx.Err())
			return
		}
		if err != nil {
			r.log.Error(err, "Script crashed, restarting", "backoff", backoff)
		} else {
			r.log.Info("Script exited, restarting", "backoff", backoff)
		}
		if time.Since(started) > 5*time.Minute {
			backoff = time.Second // stable run: reset backoff
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}

func (r *runner) runOnce(ctx context.Context) error {
	file, src, err := r.svc.load(r.name)
	if err != nil {
		return err
	}

	stateFile := filepath.Join(r.svc.cfg.StateDir, r.name+".json")
	state, err := script.LoadDeviceState(r.log, stateFile)
	if err != nil {
		return err
	}
	state.OnModified = func() {
		if err := script.SaveDeviceState(r.log, stateFile, state); err != nil {
			r.log.Error(err, "Failed to save script state", "file", stateFile)
		}
	}

	// Fresh handler maps for the fresh VM
	r.invokeHandlers = make(map[string]goja.Callable)
	r.verbHandlers = make(map[myhome.Verb]goja.Callable)
	if r.registeredVerbs == nil {
		r.registeredVerbs = make(map[myhome.Verb]bool)
	}

	ctx = logr.NewContext(ctx, r.log)
	eng, err := script.NewEngine(ctx, file, src, script.EngineOptions{
		State:               state,
		EnableExternalCalls: true,
		Customize: func(vm *goja.Runtime) error {
			return r.installMyHomeAPI(ctx, vm)
		},
	})
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.eng = eng
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.eng = nil
		r.mu.Unlock()
		// Persist final state on shutdown
		if err := script.SaveDeviceState(r.log, stateFile, state); err != nil {
			r.log.Error(err, "Failed to save script state at exit", "file", stateFile)
		}
	}()

	err = eng.Start(ctx)
	if err == context.Canceled {
		return nil
	}
	return err
}

// load resolves a script by name: user dir first, then embedded library.
func (s *Service) load(name string) (string, []byte, error) {
	file := name + ".js"
	if s.cfg.Dir != "" {
		p := filepath.Join(s.cfg.Dir, file)
		if data, err := os.ReadFile(p); err == nil {
			s.log.Info("Loaded script from user dir", "script", file, "path", p)
			return file, data, nil
		}
	}
	if s.embedded != nil {
		if data, err := fs.ReadFile(s.embedded, file); err == nil {
			return file, data, nil
		}
	}
	return "", nil, fmt.Errorf("script %s not found (dir=%q, embedded library)", file, s.cfg.Dir)
}

func normalizeName(name string) string {
	return strings.TrimSuffix(name, ".js")
}
