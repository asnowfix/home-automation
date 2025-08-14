package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"myhome/storage"
	"mynet"
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
		start := time.Now()
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			http.Error(w, "missing hostname in path", http.StatusBadRequest)
			return
		}
		parts := strings.SplitN(path, "/", 2)
		hostToken := parts[0]
		var rest string
		if len(parts) == 2 {
			rest = parts[1]
		} else {
			rest = ""
		}

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
		}

		proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, e error) {
			log.Error(e, "proxy error", "host", hostToken, "backend", targetURL.String())
			http.Error(rw, "proxy error", http.StatusBadGateway)
		}

		proxy.ServeHTTP(w, r)

		log.Info("proxied", "host", hostToken, "backend", targetURL.String(), "path", "/"+rest, "dur", time.Since(start))
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
