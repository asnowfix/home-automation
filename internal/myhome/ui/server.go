package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"global"
	"myhome/mqtt"
	mynet "myhome/net"
	"myhome/proxy"
	"myhome/storage"

	"github.com/go-logr/logr"
)

// Start launches an HTTP reverse proxy that listens on the given port and proxies
// requests shaped like: http://<listen_host>:<port>/<hostname>/<path...>
// to: http://<resolved-ip>:80/<path...>
//
// <hostname> can be:
// - an IPv4/IPv6 address
// - a .local hostname
// - any known identifier in the myhome database (name, id, mac, host)
func Start(ctx context.Context, log logr.Logger, listenPort int, resolver mynet.Resolver, db *storage.DeviceStorage, mc mqtt.Client, sseBroadcaster *SSEBroadcaster) error {
	addr := fmt.Sprintf(":%d", listenPort)
	srv := &http.Server{Addr: addr}

	log.V(1).Info("Starting UI server", "addr", addr, "resolver", resolver, "db", db, "mc", mc, "sseBroadcaster", sseBroadcaster)

	mux := http.NewServeMux()

	// Static assets with long cache
	fileServer, err := StaticFileServer()
	if err != nil {
		return fmt.Errorf("ui static file server: %w", err)
	}
	mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		// // Cache aggressively; bump version query to invalidate
		// w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.StripPrefix("/static/", fileServer).ServeHTTP(w, r)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !global.PanicOnBugs {
			// Panic recovery to avoid blank pages
			defer func() {
				if rec := recover(); rec != nil {
					log.Error(fmt.Errorf("%v", rec), "panic recovered", "path", r.URL.Path, "stack", string(debug.Stack()))
					http.Error(w, "internal error", http.StatusInternalServerError)
				}
			}()
		}

		start := time.Now()
		path := strings.TrimPrefix(r.URL.Path, "/")
		log.Info("request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr, "ua", r.UserAgent())

		if path == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := RenderIndex(ctx, db, w); err != nil {
				log.Error(err, "failed to render index page")
				http.Error(w, "unable to render index", http.StatusInternalServerError)
			}
			log.Info("served index", "dur", time.Since(start))
			return
		}

		proxy.Handle(ctx, log, resolver, db, w, r)

	})

	mux.Handle("/events", sseBroadcaster)
	mux.HandleFunc("/rpc", RpcHandler(ctx, log.WithName("RpcHandler")))

	// HTMX endpoints for partial HTML responses
	htmxHandler := NewHTMXHandler(ctx, log.WithName("HTMXHandler"), db)
	mux.HandleFunc("/htmx/devices", htmxHandler.DeviceCards)
	mux.HandleFunc("/htmx/device/", htmxHandler.DeviceCard)
	mux.HandleFunc("/htmx/rooms", htmxHandler.RoomsList)
	mux.HandleFunc("/htmx/switch/toggle", htmxHandler.SwitchButton)

	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Info("http-incoming", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr, "proto", r.Proto, "content-length", r.ContentLength)
		mux.ServeHTTP(w, r)
	})

	// Start server
	go func() {
		log.Info("UI server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error(err, "UI server failed")
		} else {
			log.Info("UI server stopped")
		}
	}()

	// Shutdown on context cancel
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		log.V(1).Info("UI server shutdown")
	}()

	return nil
}

// statusWriter captures response status code and bytes written
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		// default if WriteHeader not called
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Ensure websocket upgrades can hijack the connection through this wrapper
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not support hijacking")
}

// Pass through Flush when supported
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
