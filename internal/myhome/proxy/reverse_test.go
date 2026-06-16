package proxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
)

// stubResolver implements mynet.Resolver for tests.
// LookupHost returns the pre-seeded entries; everything else is a no-op.
type stubResolver struct {
	hosts map[string][]net.IP
}

func (r *stubResolver) WithLocalName(_ context.Context, _ string) interface{ LookupHost(context.Context, logr.Logger, string) ([]net.IP, error) } {
	return r
}
func (r *stubResolver) LookupHost(_ context.Context, _ logr.Logger, host string) ([]net.IP, error) {
	if ips, ok := r.hosts[host]; ok {
		return ips, nil
	}
	return nil, net.UnknownNetworkError("not found")
}

// resolverOnly wraps stubResolver to satisfy the mynet.Resolver interface.
// We only need the LookupHost method for resolveToIPv4, so we embed enough.
type resolverOnly struct{ stubResolver }

func (r *resolverOnly) BrowseService(ctx context.Context, service, domain string, entries chan interface{}) error {
	return nil
}
func (r *resolverOnly) PublishService(ctx context.Context, instance, service, domain string, port int, txt []string, ifaces []net.Interface) (interface{}, error) {
	return nil, nil
}
func (r *resolverOnly) LookupService(ctx context.Context, service string) (interface{}, error) {
	return nil, nil
}
func (r *resolverOnly) WithLocalName(ctx context.Context, hostname string) interface{} { return r }

// minimalResolver is a thin wrapper that satisfies only the subset of
// mynet.Resolver used by resolveToIPv4.
type minimalResolver struct {
	hosts map[string][]net.IP
}

func (m *minimalResolver) LookupHost(_ context.Context, _ logr.Logger, host string) ([]net.IP, error) {
	if ips, ok := m.hosts[host]; ok {
		return ips, nil
	}
	return nil, net.UnknownNetworkError("not found")
}

// TestResolveToIPv4_RawIP verifies that a plain IPv4 string is returned as-is.
func TestResolveToIPv4_RawIP(t *testing.T) {
	ctx := context.Background()
	log := testr.New(t)

	ip, err := resolveToIPv4(ctx, log, nil, nil, "192.168.1.42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(net.ParseIP("192.168.1.42").To4()) {
		t.Errorf("got %v, want 192.168.1.42", ip)
	}
}

// TestResolveToIPv4_IPv6Rejected verifies that a pure IPv6 address is rejected.
func TestResolveToIPv4_IPv6Rejected(t *testing.T) {
	ctx := context.Background()
	log := testr.New(t)

	_, err := resolveToIPv4(ctx, log, nil, nil, "::1")
	if err == nil {
		t.Fatal("expected error for IPv6, got nil")
	}
}

// TestResolveToIPv4_NilResolver_Unknown verifies that an unknown hostname
// returns an error when resolver and db are both nil.
func TestResolveToIPv4_NilResolver_Unknown(t *testing.T) {
	ctx := context.Background()
	log := testr.New(t)

	_, err := resolveToIPv4(ctx, log, nil, nil, "unknown-host")
	if err == nil {
		t.Fatal("expected error for unresolvable hostname, got nil")
	}
}

// TestStatusWriter_WriteHeader_and_Write verifies that statusWriter tracks the
// HTTP status code and byte count correctly.
func TestStatusWriter_WriteHeader_and_Write(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}

	// WriteHeader should capture the status code
	sw.WriteHeader(http.StatusNotFound)
	if sw.status != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, sw.status)
	}

	// Write should accumulate the byte count
	n, err := sw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != 5 {
		t.Errorf("Write returned %d, want 5", n)
	}
	if sw.bytes != 5 {
		t.Errorf("bytes = %d, want 5", sw.bytes)
	}
}

// TestStatusWriter_Write_DefaultStatus verifies that writing without calling
// WriteHeader sets the default status to 200.
func TestStatusWriter_Write_DefaultStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}

	_, _ = sw.Write([]byte("x"))
	if sw.status != http.StatusOK {
		t.Errorf("expected default status %d, got %d", http.StatusOK, sw.status)
	}
}

// TestStatusWriter_Flush verifies that Flush is a no-op when the underlying
// ResponseWriter does not implement http.Flusher (httptest.ResponseRecorder does).
func TestStatusWriter_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	sw.Flush() // must not panic
}

// TestStatusWriter_Hijack_NotSupported verifies that Hijack returns an error
// when the underlying ResponseWriter is not a http.Hijacker.
func TestStatusWriter_Hijack_NotSupported(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	_, _, err := sw.Hijack()
	if err == nil {
		t.Fatal("expected error from Hijack on non-Hijacker ResponseWriter")
	}
}

// TestHandle_Health exercises the _health fast-path, requiring no resolver, db,
// or upstream proxy.
func TestHandle_Health(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/_health", nil)
	rec := httptest.NewRecorder()
	log := testr.New(t)
	ctx := context.Background()

	Handle(ctx, log, nil, nil, "", rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "OK") {
		t.Errorf("want body containing OK, got %q", body)
	}
}

// TestHandle_UnknownRootPath exercises the strict 404 for non-/devices/ paths.
func TestHandle_UnknownRootPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/some/unknown/path", nil)
	rec := httptest.NewRecorder()
	log := testr.New(t)
	ctx := context.Background()

	Handle(ctx, log, nil, nil, "", rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

// TestHandle_DevicesPath_NoResolver exercises the /devices/{token}/... path
// when the host cannot be resolved, expecting a 502.
func TestHandle_DevicesPath_NoResolver(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/devices/nonexistent-device/api/v1", nil)
	rec := httptest.NewRecorder()
	log := testr.New(t)
	ctx := context.Background()

	Handle(ctx, log, nil, nil, "", rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("want 502, got %d", rec.Code)
	}
}

// TestHandle_UpstreamProxy_Malformed verifies that a malformed upstream proxy
// URL returns 500.
func TestHandle_UpstreamProxy_Malformed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/devices/device1/api", nil)
	rec := httptest.NewRecorder()
	log := testr.New(t)
	ctx := context.Background()

	Handle(ctx, log, nil, nil, "://bad-url", rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", rec.Code)
	}
}
