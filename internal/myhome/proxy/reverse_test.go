package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	mynet "github.com/asnowfix/home-automation/internal/myhome/net"
	"github.com/asnowfix/home-automation/myhome/storage"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/grandcat/zeroconf"
)

// fakeResolver implements mynet.Resolver, returning a canned LookupHost
// result keyed by the queried host. Other methods are unused by
// resolveToIPv4 and just satisfy the interface.
type fakeResolver struct {
	byHost map[string][]net.IP
}

func (f *fakeResolver) WithLocalName(ctx context.Context, hostname string) mynet.Resolver { return f }
func (f *fakeResolver) LookupHost(ctx context.Context, log logr.Logger, host string) ([]net.IP, error) {
	if ips, ok := f.byHost[host]; ok {
		return ips, nil
	}
	return nil, fmt.Errorf("no such host: %s", host)
}
func (f *fakeResolver) LookupService(ctx context.Context, service string) (*url.URL, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *fakeResolver) BrowseService(ctx context.Context, service, domain string, entries chan<- *zeroconf.ServiceEntry) error {
	return fmt.Errorf("not implemented")
}
func (f *fakeResolver) PublishService(ctx context.Context, instance, service, domain string, port int, txt []string, ifaces []net.Interface) (*zeroconf.Server, error) {
	return nil, fmt.Errorf("not implemented")
}

// newTestStorage returns an in-memory DeviceStorage seeded with one device
// whose Host is empty (as devices are stored post-#252: no cached IPs).
func newTestStorage(t *testing.T, log logr.Logger, id, name string) *storage.DeviceStorage {
	t.Helper()
	s, err := storage.NewDeviceStorage(log, ":memory:")
	if err != nil {
		t.Fatalf("failed to create test storage: %v", err)
	}
	t.Cleanup(s.Close)

	dev := &myhome.Device{
		DeviceSummary: myhome.DeviceSummary{
			DeviceIdentifier: myhome.DeviceIdentifier{Manufacturer_: "shelly", Id_: id},
			Name_:            name,
		},
	}
	if _, err := s.SetDevice(context.Background(), dev, true); err != nil {
		t.Fatalf("failed to seed test device: %v", err)
	}
	return s
}

// TestResolveToIPv4_FallsBackToDeviceIdThenName verifies that with an empty
// (post-#252) device.Host, resolution tries the device ID first (the form
// that matches its mDNS "<id>.local" name), and falls back to Name only if
// ID resolution fails.
func TestResolveToIPv4_FallsBackToDeviceIdThenName(t *testing.T) {
	ctx := context.Background()
	log := testr.New(t)

	s := newTestStorage(t, log, "shellyplus1pm-aabbccddeeff", "pump")
	resolver := &fakeResolver{byHost: map[string][]net.IP{
		"shellyplus1pm-aabbccddeeff": {net.ParseIP("192.168.1.77")},
	}}

	ip, err := resolveToIPv4(ctx, log, resolver, s, "pump")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(net.ParseIP("192.168.1.77")) {
		t.Errorf("got %v, want 192.168.1.77", ip)
	}
}

func TestResolveToIPv4_FallsBackToNameWhenIdUnresolvable(t *testing.T) {
	ctx := context.Background()
	log := testr.New(t)

	s := newTestStorage(t, log, "shellyplus1pm-aabbccddeeff", "pump")
	resolver := &fakeResolver{byHost: map[string][]net.IP{
		"pump": {net.ParseIP("192.168.1.88")},
	}}

	ip, err := resolveToIPv4(ctx, log, resolver, s, "pump")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(net.ParseIP("192.168.1.88")) {
		t.Errorf("got %v, want 192.168.1.88", ip)
	}
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
