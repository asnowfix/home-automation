package http

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/asnowfix/home-automation/pkg/shelly/typestest"

	"github.com/go-logr/logr"
)

// fakeResolverFunc adapts a plain func to types.HostResolver for tests.
type fakeResolverFunc func(ctx context.Context, mac net.HardwareAddr, name string) (net.IP, error)

func (f fakeResolverFunc) ResolveHost(ctx context.Context, mac net.HardwareAddr, name string) (net.IP, error) {
	return f(ctx, mac, name)
}

func withLogger(ctx context.Context) context.Context {
	return logr.NewContext(ctx, logr.Discard())
}

// TestCallE_SucceedsWithoutResolution verifies the plain happy path is
// unaffected by the retry-on-failure logic: a device whose Host is already
// dialable gets called once, no resolver involved.
func TestCallE_SucceedsWithoutResolution(t *testing.T) {
	t.Cleanup(func() { types.SetHostResolver(nil) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	device := typestest.NewFakeDevice()
	device.IdValue = "shellyplus1pm-aabbccddeeff"
	device.HostValue = srv.Listener.Addr().String()

	verb := types.MethodHandler{Method: "Shelly.GetStatus", HttpMethod: http.MethodGet}
	out := map[string]any{}

	_, err := httpChannel.callE(withLogger(context.Background()), device, verb, &out, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if device.Host() != srv.Listener.Addr().String() {
		t.Errorf("host should be unchanged on success, got %q", device.Host())
	}
}

// TestCallE_RetriesOnceAfterReResolution verifies that on a dial failure,
// callE clears the stale host, asks the installed resolver exactly once,
// updates the host with what it returns, and retries — and that if the
// retry also fails, the host ends up cleared again.
func TestCallE_RetriesOnceAfterReResolution(t *testing.T) {
	t.Cleanup(func() { types.SetHostResolver(nil) })

	resolveCalls := 0
	types.SetHostResolver(fakeResolverFunc(func(ctx context.Context, mac net.HardwareAddr, name string) (net.IP, error) {
		resolveCalls++
		return net.ParseIP("127.0.0.1"), nil
	}))

	device := typestest.NewFakeDevice()
	device.IdValue = "shellyplus1pm-aabbccddeeff"
	// Port 1 is reserved/unassigned: connection is refused immediately on
	// both the first attempt and the retry (the resolver only returns a bare
	// IP, so the retry dials it on the default port-less host string, which
	// is equally unreachable here) — good enough to exercise the retry path
	// without depending on a real listener on the resolved address.
	device.HostValue = "127.0.0.1:1"

	verb := types.MethodHandler{Method: "Shelly.GetStatus", HttpMethod: http.MethodGet}
	out := map[string]any{}

	_, err := httpChannel.callE(withLogger(context.Background()), device, verb, &out, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if resolveCalls != 1 {
		t.Errorf("expected resolver to be called exactly once, got %d", resolveCalls)
	}
	if device.Host() != "" {
		t.Errorf("expected host to be cleared after retry also fails, got %q", device.Host())
	}
}

// TestCallE_NoResolverClearsHostOnFailure verifies the pre-existing behavior
// (clear host, give the caller an error to fall back to MQTT) is preserved
// when no resolver is installed.
func TestCallE_NoResolverClearsHostOnFailure(t *testing.T) {
	t.Cleanup(func() { types.SetHostResolver(nil) })
	types.SetHostResolver(nil)

	device := typestest.NewFakeDevice()
	device.IdValue = "shellyplus1pm-aabbccddeeff"
	device.HostValue = "127.0.0.1:1"

	verb := types.MethodHandler{Method: "Shelly.GetStatus", HttpMethod: http.MethodGet}
	out := map[string]any{}

	_, err := httpChannel.callE(withLogger(context.Background()), device, verb, &out, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if device.Host() != "" {
		t.Errorf("expected host to be cleared, got %q", device.Host())
	}
}
