package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"myhome/storage"
	"mynet"

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
func Start(ctx context.Context, log logr.Logger, listenPort int, resolver mynet.Resolver, db *storage.DeviceStorage) error {
	addr := fmt.Sprintf(":%d", listenPort)
	srv := &http.Server{Addr: addr}

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

		targetURL, _ := url.Parse("http://" + targetIP.String())
		proxy := httputil.NewSingleHostReverseProxy(targetURL)

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
				backendOrigin := "http://" + targetURL.Host
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

	srv.Handler = handler

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
  <style>
    :root { color-scheme: light dark; }
    body { font-family: system-ui, -apple-system, Segoe UI, Roboto, Ubuntu, Cantarell, Noto Sans, sans-serif; margin: 2rem; }
    h1 { margin-bottom: 0.5rem; }
    .subtitle { color: #6b7280; margin-bottom: 1.5rem; }
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 1rem; }
    .card { border: 1px solid #e5e7eb33; border-radius: 12px; padding: 1rem; background: #ffffff0d; backdrop-filter: blur(2px); }
    .name { font-weight: 600; font-size: 1.05rem; margin-bottom: 0.25rem; }
    .meta { font-size: 0.9rem; color: #6b7280; }
    a.button { display:inline-block; margin-top:0.6rem; padding:0.4rem 0.7rem; border-radius:8px; text-decoration:none; background:#2563eb; color:white; }
    .empty { color:#6b7280; font-style: italic; }
    footer { margin-top:2rem; font-size: 0.85rem; color:#9ca3af; }
  </style>
  <link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='0.9em' font-size='90'>🏠</text></svg>">
  </head>
<body>
  <h1>MyHome</h1>
  <div class="subtitle">Known devices ({{len .Devices}})</div>
  {{if .Devices}}
  <div class="grid">
    {{range .Devices}}
    <div class="card">
      <div class="name">{{.Name}}</div>
      <div class="meta">{{.Manufacturer}} · {{.Id}}</div>
      {{if .Host}}
        <div class="meta">Host: {{.Host}}</div>
        <a class="button" href="/devices/{{.LinkToken}}/" target="_blank" rel="noopener noreferrer">Open</a>
      {{else}}
        <div class="meta">No host known</div>
      {{end}}
    </div>
    {{end}}
  </div>
  {{else}}
    <p class="empty">No devices found.</p>
  {{end}}
  <footer>Served by MyHome reverse proxy</footer>
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
