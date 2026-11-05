package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"
)

// countingTLSBackend returns a self-signed HTTPS backend and a counter of the
// TCP connections it has accepted.
func countingTLSBackend(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var opened atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello")
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			opened.Add(1)
		}
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, &opened
}

func proxyOnce(t *testing.T, target *url.URL, rt http.RoundTripper) {
	t.Helper()
	p := httputil.NewSingleHostReverseProxy(target)
	p.Transport = rt
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy status = %d, body %q", rec.Code, rec.Body.String())
	}
}

// TestInsecureTransport_ReusesConnection is the regression test for #695: the
// HTTPS proxy path used to build a fresh http.Transport per request, leaving
// one never-closed idle connection (and goroutine pair) behind each time. The
// shared transport must serve sequential requests over a single connection and
// accept the backend's self-signed certificate.
func TestInsecureTransport_ReusesConnection(t *testing.T) {
	srv, opened := countingTLSBackend(t)
	target, _ := url.Parse(srv.URL)
	t.Cleanup(insecureTransport.CloseIdleConnections)

	const calls = 8
	for i := 0; i < calls; i++ {
		proxyOnce(t, target, insecureTransport)
	}
	if got := opened.Load(); got != 1 {
		t.Errorf("%d connections opened for %d sequential requests, want 1", got, calls)
	}
}

// TestInsecureTransport_Config pins the settings that bound idle connections.
func TestInsecureTransport_Config(t *testing.T) {
	if insecureTransport.IdleConnTimeout <= 0 {
		t.Error("IdleConnTimeout must be set so idle connections are eventually closed")
	}
	if insecureTransport.TLSClientConfig == nil || !insecureTransport.TLSClientConfig.InsecureSkipVerify {
		t.Error("self-signed device certificates must remain accepted")
	}
}
