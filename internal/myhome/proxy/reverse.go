package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"global"
	"myhome/storage"
	"myhome/ui/assets"
	"myhome/ui/static"
	"mynet"

	"github.com/go-logr/logr"
)

// Setup adds a reverse proxy handler to the given mux that proxies
// requests shaped like: http://<listen_host>:<port>/<hostname>/<path...>
// to: http://<resolved-ip>:80/<path...>
//
// <hostname> can be:
// - an IPv4/IPv6 address
// - a .local hostname
// - any known identifier in the myhome database (name, id, mac, host)
func Handle(ctx context.Context, log logr.Logger, resolver mynet.Resolver, db *storage.DeviceStorage, w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Panic recovery to avoid blank pages
	if !global.PanicOnBugs {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error(fmt.Errorf("%v", rec), "panic recovered", "path", r.URL.Path, "stack", string(debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	log.Info("request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr, "ua", r.UserAgent())

	// health endpoint for quick checks
	if path == "_health" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "OK")
		return
	}

	// Serve global static websocket patch resource for caching
	if path == "_ws_patch.js" {
		log.Info("_ws_patch.js", "path", "/"+path)
		buf := assets.GetWsPatch()
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		//TODO: tune caching
		//w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = w.Write(buf)
		return
	}

	if path == "bulma.min.css" {
		log.Info("bulma.min.css", "path", "/"+path)
		buf := static.GetBulmaCSS()
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		//TODO: tune caching
		//w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
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
