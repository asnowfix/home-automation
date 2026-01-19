package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"myhome"
	"myhome/storage"
	"mynet"

	"github.com/go-logr/logr"
)

// Embed static assets under this package
//
//go:embed static/*
var staticFS embed.FS

// Start launches an HTTP reverse proxy that listens on the given port and proxies
// requests shaped like: http://<listen_host>:<port>/<hostname>/<path...>
// to: http://<resolved-ip>:80/<path...>
//
// <hostname> can be:
// - an IPv4/IPv6 address
// - a .local hostname
// - any known identifier in the myhome database (name, id, mac, host)
func Start(ctx context.Context, log logr.Logger, listenPort int, resolver mynet.Resolver, db *storage.DeviceStorage) error {
	addr := fmt.Sprintf(":%d", listenPort)
	srv := &http.Server{Addr: addr}

	// Simple in-process SSE broadcaster for device refresh completion
	type sseClient chan string
	var sse struct {
		mu      sync.Mutex
		clients map[sseClient]struct{}
	}
	sse.clients = make(map[sseClient]struct{})

	notifyRefresh := func(id string) {
		msg := fmt.Sprintf("event: device-refresh\ndata: %s\n\n", id)
		sse.mu.Lock()
		defer sse.mu.Unlock()
		for ch := range sse.clients {
			select {
			case ch <- msg:
			default:
			}
		}
	}

	mux := http.NewServeMux()

	// Static assets with long cache
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("embed fs subdir: %w", err)
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		// Cache aggressively; bump version query to invalidate
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.StripPrefix("/static/", fileServer).ServeHTTP(w, r)
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Panic recovery to avoid blank pages
		defer func() {
			if rec := recover(); rec != nil {
				log.Error(fmt.Errorf("%v", rec), "panic recovered", "path", r.URL.Path, "stack", string(debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()

		start := time.Now()
		path := strings.TrimPrefix(r.URL.Path, "/")
		log.Info("request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr, "ua", r.UserAgent())
		if path == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := renderIndex(ctx, db, w); err != nil {
				log.Error(err, "failed to render index page")
				http.Error(w, "unable to render index", http.StatusInternalServerError)
			}
			log.Info("served index", "dur", time.Since(start))
			return
		}

		// health endpoint for quick checks
		if path == "_health" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, "OK")
			return
		}

		// Serve global static websocket patch resource for caching
		if path == "_ws_patch.js" {
			buf := getWsPatch()
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			_, _ = w.Write(buf)
			return
		}

		// RPC endpoint to call device manager methods (e.g., device.refresh)
		if path == "rpc" && r.Method == http.MethodPost {
			var req struct {
				Method myhome.Verb `json:"method"`
				Params any         `json:"params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			mh, err := myhome.Methods(req.Method)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			res, err := mh.ActionE(req.Params)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
				return
			}
			// Successful call: if it's a device.refresh with string id, notify SSE listeners
			if req.Method == myhome.DeviceRefresh {
				if id, ok := req.Params.(string); ok && id != "" {
					notifyRefresh(id)
				}
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{"result": res})
			return
		}

		// SSE events stream: /events?device=<id>
		if path == "events" && r.Method == http.MethodGet {
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "stream unsupported", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			_ = r.ParseForm()
			want := r.Form.Get("device")
			ch := make(sseClient, 4)
			sse.mu.Lock()
			sse.clients[ch] = struct{}{}
			sse.mu.Unlock()
			defer func() { sse.mu.Lock(); delete(sse.clients, ch); sse.mu.Unlock() }()
			// Send initial comment to open the stream
			_, _ = w.Write([]byte(": connected\n\n"))
			flusher.Flush()
			ctx := r.Context()
			for {
				select {
				case <-ctx.Done():
					return
				case msg := <-ch:
					// If a device filter is present, only forward matching events
					if want != "" {
						// msg format: lines with data: <id>
						if strings.Contains(msg, "data: "+want+"\n") {
							_, _ = w.Write([]byte(msg))
							flusher.Flush()
						}
					} else {
						_, _ = w.Write([]byte(msg))
						flusher.Flush()
					}
				}
			}
		}

		// New routing scheme: /devices/{hostToken}/... (strict)
		var hostToken string
		var rest string
		if strings.HasPrefix(path, "devices/") {
			after := strings.TrimPrefix(path, "devices/")
			parts := strings.SplitN(after, "/", 2)
			hostToken = parts[0]
			if len(parts) == 2 {
				rest = parts[1]
			} else {
				rest = ""
			}
		} else {
			// Strict mode: only /devices/... is supported (plus static endpoints above)
			log.Info("unsupported-path", "path", "/"+path)
			http.NotFound(w, r)
			return
		}

		log.Info("route", "hostToken", hostToken, "rest", rest)

		targetIP, err := resolveToIPv4(ctx, resolver, db, hostToken)
		if err != nil {
			log.Error(err, "failed to resolve host", "host", hostToken)
			http.Error(w, "unable to resolve host", http.StatusBadGateway)
			return
		}

		// Decide backend scheme by probing ports: try 80 (http), else 443 (https)
		scheme := "http"
		d80, _ := net.DialTimeout("tcp", net.JoinHostPort(targetIP.String(), "80"), 600*time.Millisecond)
		if d80 != nil {
			_ = d80.Close()
		} else {
			d443, _ := net.DialTimeout("tcp", net.JoinHostPort(targetIP.String(), "443"), 800*time.Millisecond)
			if d443 != nil {
				_ = d443.Close()
				scheme = "https"
			} else {
				log.Error(fmt.Errorf("no http/https service"), "backend ports closed", "ip", targetIP.String())
				http.Error(w, "backend not reachable on 80/443", http.StatusBadGateway)
				return
			}
		}

		targetURL, _ := url.Parse(scheme + "://" + targetIP.String())
		proxy := httputil.NewSingleHostReverseProxy(targetURL)
		if scheme == "https" {
			// accept self-signed device certs
			if tp, ok := proxy.Transport.(*http.Transport); ok && tp != nil {
				// unexpected: ReverseProxy.Transport is nil by default, so ok==false
			}
			proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		}

		// Customize director to preserve the remainder path and query
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			// Rewrite path to the remainder
			req.URL.Path = "/" + rest
			// Ensure Host header targets backend
			req.Host = targetURL.Host

			// If this is a WebSocket upgrade, some backends enforce Origin. Rewrite it to the backend origin.
			connHdr := strings.ToLower(req.Header.Get("Connection"))
			upgHdr := strings.ToLower(req.Header.Get("Upgrade"))
			if strings.Contains(connHdr, "upgrade") || upgHdr == "websocket" {
				backendOrigin := scheme + "://" + targetURL.Host
				if got := req.Header.Get("Origin"); got != backendOrigin {
					log.Info("ws-origin-rewrite", "from", got, "to", backendOrigin, "path", "/"+rest)
					req.Header.Set("Origin", backendOrigin)
				}
				// Log selected request headers for troubleshooting
				log.Info("ws-request", "path", "/"+rest, "Host", req.Host, "Origin", req.Header.Get("Origin"), "Sec-WebSocket-Protocol", req.Header.Get("Sec-WebSocket-Protocol"), "Version", req.Header.Get("Sec-WebSocket-Version"))
			}
		}

		// Rewrite redirects and HTML so navigation stays under /{hostToken}/
		proxy.ModifyResponse = func(resp *http.Response) error {
			// 1) Location header rewrite for redirects
			if loc := resp.Header.Get("Location"); loc != "" {
				if u, err := url.Parse(loc); err == nil {
					prefix := "/devices/" + hostToken
					if u.IsAbs() {
						// Absolute redirect: rewrite if it targets the proxy host OR the backend host
						backendHost := targetURL.Host
						sameProxy := resp.Request != nil && u.Host == resp.Request.Host
						sameBackend := u.Host == backendHost || u.Host == backendHost+":80"
						if sameProxy || sameBackend {
							if u.Path == "" {
								u.Path = "/"
							}
							if strings.HasPrefix(u.Path, "/") {
								u.Path = prefix + u.Path
							}
							u.Scheme = ""
							u.Host = ""
							resp.Header.Set("Location", u.String())
						}
					} else {
						// Relative URL
						if strings.HasPrefix(u.Path, "/") {
							u.Path = prefix + u.Path
							resp.Header.Set("Location", u.String())
						}
					}
				}
			}

			// Log response headers for WS handshake outcomes
			if resp.StatusCode == http.StatusSwitchingProtocols || strings.ToLower(resp.Header.Get("Upgrade")) == "websocket" {
				log.Info("ws-response", "status", resp.StatusCode, "Connection", resp.Header.Get("Connection"), "Upgrade", resp.Header.Get("Upgrade"), "Sec-WebSocket-Accept", resp.Header.Get("Sec-WebSocket-Accept"), "Sec-WebSocket-Protocol", resp.Header.Get("Sec-WebSocket-Protocol"))
			}

			// 2) HTML body rewrite: add <base> and fix root-relative asset links
			ct := resp.Header.Get("Content-Type")
			if strings.Contains(strings.ToLower(ct), "text/html") && resp.Body != nil {
				// Handle gzip-compressed HTML transparently
				ce := strings.ToLower(resp.Header.Get("Content-Encoding"))
				isGzip := strings.Contains(ce, "gzip")
				var raw []byte
				if isGzip {
					gr, err := gzip.NewReader(resp.Body)
					if err != nil {
						return nil
					}
					defer gr.Close()
					raw, err = io.ReadAll(gr)
					if err != nil {
						return nil
					}
					_ = resp.Body.Close()
				} else {
					b, err := io.ReadAll(resp.Body)
					if err != nil {
						return nil
					}
					_ = resp.Body.Close()
					raw = b
				}
				h := string(raw)
				origLen := len(h)
				prefix := "/devices/" + hostToken + "/"
				// Inject <base> if not present
				headRe := regexp.MustCompile(`(?i)<head[^>]*>`) // first <head>
				baseRe := regexp.MustCompile(`(?i)<base\s+href=`)
				if headRe.MatchString(h) && !baseRe.MatchString(h) {
					h = headRe.ReplaceAllString(h, "$0\n  <base href=\""+prefix+"\">")
				}
				// Rewrite root-relative href/src/action attributes (avoid protocol-relative //)
				// Go's regexp does not support lookaheads, so we ensure the first char after
				// the leading slash is not another slash by matching it explicitly.
				attrRe := regexp.MustCompile(`(?i)(\\b(?:href|src|action)=[\"'])/([^/][^\"'>]*)([\"'])`)
				h = attrRe.ReplaceAllString(h, "$1"+prefix+"$2$3")

				// Inject a reference to the global websocket patch (static, cacheable)
				scriptTag := "<script src=\"/_ws_patch.js\" defer></script>"

				// Prefer injecting right after <head>, otherwise before </body>
				if headRe.MatchString(h) {
					h = headRe.ReplaceAllString(h, "$0\n  "+scriptTag)
				} else {
					endBodyRe := regexp.MustCompile(`(?i)</body>`)
					if endBodyRe.MatchString(h) {
						h = endBodyRe.ReplaceAllString(h, scriptTag+"\n</body>")
					} else {
						// Fallback: append at end
						h += "\n" + scriptTag
					}
				}

				if isGzip {
					var buf bytes.Buffer
					gw := gzip.NewWriter(&buf)
					_, _ = gw.Write([]byte(h))
					_ = gw.Close()
					resp.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
					resp.Header.Set("Content-Encoding", "gzip")
					resp.Header.Set("Content-Length", strconv.Itoa(buf.Len()))
				} else {
					resp.Body = io.NopCloser(bytes.NewReader([]byte(h)))
					resp.Header.Del("Content-Length") // will be re-calculated by Go
				}
				log.Info("html-rewrite", "ct", ct, "orig_len", origLen, "new_len", len(h))
			}
			return nil
		}

		proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, e error) {
			log.Error(e, "proxy error", "host", hostToken, "backend", targetURL.String())
			http.Error(rw, "proxy error", http.StatusBadGateway)
		}

		// Detect websocket upgrades
		if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") || strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
			log.Info("websocket-upgrade", "host", hostToken, "path", "/"+rest)
		}

		// Capture status/bytes
		sw := &statusWriter{ResponseWriter: w, status: 0}
		proxy.ServeHTTP(sw, r)

		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		log.Info("proxied", "host", hostToken, "backend", targetURL.String(), "path", "/"+rest, "status", sw.status, "bytes", sw.bytes, "dur", time.Since(start))
	})

	mux.Handle("/", handler)

	srv.Handler = mux

	// Start server
	go func() {
		log.Info("Reverse proxy listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error(err, "reverse proxy server failed")
		}
	}()

	// Shutdown on context cancel
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		log.Info("Reverse proxy stopped")
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

// index template and renderer
var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>MyHome Devices</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bulma@0.9.4/css/bulma.min.css"/>
  <link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='0.9em' font-size='90'>🏠</text></svg>"/>
  </head>
<body>
  <section class="hero is-light is-small">
    <div class="hero-body">
      <div class="container">
        <div class="level">
          <div class="level-left">
            <h1 class="title is-3">MyHome</h1>
            <span class="subtitle is-6 ml-3">Known devices ({{len .Devices}})</span>
          </div>
          <div class="level-right">
            <a class="button is-info" href="https://control.shelly.cloud" target="_blank" rel="noopener noreferrer">Shelly Control</a>
            <a class="button is-link ml-2" href="https://community.shelly.cloud/" target="_blank" rel="noopener noreferrer">Community Forum</a>
          </div>
        </div>
      </div>
    </div>
  </section>
  <section class="section">
    <div class="container">
      {{if .Devices}}
      <div class="columns is-multiline">
        {{range .Devices}}
        <div class="column is-4-desktop is-6-tablet">
          <div class="card">
            <div class="card-content">
              <p class="title is-5">{{.Name}}</p>
              <p class="subtitle is-7 has-text-grey">{{.Manufacturer}} · {{.Id}}</p>
              {{if .Host}}
                <p class="has-text-grey">Host: {{.Host}}</p>
              {{else}}
                <p class="has-text-grey">No host known</p>
              {{end}}
              <div class="buttons mt-3">
                {{if .Host}}
                  <a class="button is-link" href="/devices/{{.LinkToken}}/" target="_blank" rel="noopener noreferrer">Open</a>
                {{end}}
                <button class="button is-warning" id="btn-{{.Id}}" onclick="refreshDevice('{{.Id}}')">Refresh</button>
              </div>
            </div>
          </div>
        </div>
        {{end}}
      </div>
      {{else}}
        <div class="notification is-light has-text-grey is-size-6">No devices found.</div>
      {{end}}
    </div>
  </section>
  <footer class="footer">
    <div class="content has-text-centered has-text-grey-light is-size-7">
      Served by MyHome reverse proxy
    </div>
  </footer>
  <script>
    async function refreshDevice(id) {
      const btn = document.getElementById('btn-' + id);
      if (btn) { btn.disabled = true; btn.textContent = 'Refreshing…'; }
      let es;
      try {
        es = new EventSource('/events?device=' + encodeURIComponent(id));
        es.addEventListener('device-refresh', function(ev) {
          if (btn) { btn.textContent = 'Done'; }
          try { es.close(); } catch {}
          location.reload();
        });
        es.onerror = function() { /* keep open or close silently */ };

        const res = await fetch('/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ method: 'device.refresh', params: id })
        });
        if (!res.ok) {
          const t = await res.text();
          if (btn) { btn.disabled = false; btn.textContent = 'Refresh'; }
          alert('Refresh failed: ' + t);
          if (es) try { es.close(); } catch {}
          return;
        }
        // If backend completed before SSE connected, fallback: reload soon
        setTimeout(() => { if (btn) { btn.textContent = 'Done'; } location.reload(); }, 1500);
      } catch (e) {
        if (btn) { btn.disabled = false; btn.textContent = 'Refresh'; }
        if (es) try { es.close(); } catch {}
        alert('Refresh error: ' + e);
      }
    }
  </script>
</body>
</html>`))

type deviceView struct {
	Name         string
	Id           string
	Manufacturer string
	Host         string
	LinkToken    string
}

func renderIndex(ctx context.Context, db *storage.DeviceStorage, w http.ResponseWriter) error {
	data := struct{ Devices []deviceView }{Devices: []deviceView{}}
	if db != nil {
		devices, err := db.GetAllDevices(ctx)
		if err != nil {
			return indexTmpl.Execute(w, data)
		}
		for _, d := range devices {
			name := d.Name()
			if name == "" {
				name = d.Id()
			}
			host := d.Host()
			token := host
			if token == "" {
				token = d.Name()
				if token == "" {
					token = d.Id()
				}
			}
			data.Devices = append(data.Devices, deviceView{
				Name:         name,
				Id:           d.Id(),
				Manufacturer: d.Manufacturer(),
				Host:         host,
				LinkToken:    token,
			})
		}
		sort.Slice(data.Devices, func(i, j int) bool {
			return strings.ToLower(data.Devices[i].Name) < strings.ToLower(data.Devices[j].Name)
		})
	}
	return indexTmpl.Execute(w, data)
}

func resolveToIPv4(ctx context.Context, resolver mynet.Resolver, db *storage.DeviceStorage, token string) (net.IP, error) {
	// 1. If token is an IP, return it (prefer IPv4)
	if ip := net.ParseIP(token); ip != nil {
		if ip.To4() != nil {
			return ip.To4(), nil
		}
		return nil, fmt.Errorf("non-IPv4 address not supported: %s", token)
	}

	// Helper to pick first IPv4 from list
	pickV4 := func(ips []net.IP) net.IP {
		for _, ip := range ips {
			if v4 := ip.To4(); v4 != nil {
				return v4
			}
		}
		return nil
	}

	// 2. If .local, strip suffix for resolver
	query := token
	if strings.HasSuffix(strings.ToLower(query), ".local") {
		query = strings.TrimSuffix(query, ".local")
	}

	if resolver != nil {
		if ips, err := resolver.LookupHost(ctx, query); err == nil {
			if ip := pickV4(ips); ip != nil {
				return ip, nil
			}
		}
	}

	// 3. Try myhome database lookup
	if db != nil {
		if d, err := db.GetDeviceByAny(ctx, token); err == nil {
			// Prefer device.Host when present
			host := d.Host()
			if host == "" {
				// Fallbacks
				host = d.Name()
				if host == "" {
					host = d.Id()
				}
			}
			if net.ParseIP(host) != nil {
				ip := net.ParseIP(host)
				if v4 := ip.To4(); v4 != nil {
					return v4, nil
				}
			} else if resolver != nil {
				q := host
				if strings.HasSuffix(strings.ToLower(q), ".local") {
					q = strings.TrimSuffix(q, ".local")
				}
				if ips, err := resolver.LookupHost(ctx, q); err == nil {
					if ip := pickV4(ips); ip != nil {
						return ip, nil
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("unable to resolve %s to IPv4", token)
}
