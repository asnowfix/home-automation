package myip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// withSeeIPServer starts an httptest.Server, redirects seeIPURL to it for the
// duration of the test, and restores the original URL in t.Cleanup. seeIPURL
// is a package-level var, so tests using this must not call t.Parallel().
func withSeeIPServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	prev := seeIPURL
	seeIPURL = srv.URL
	t.Cleanup(func() {
		srv.Close()
		seeIPURL = prev
	})
	return srv
}

func TestSeeIp_HappyPath(t *testing.T) {
	withSeeIPServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ip":"203.0.113.42"}`))
	})

	ip, err := SeeIp(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "203.0.113.42" {
		t.Errorf("expected '203.0.113.42', got %q", ip)
	}
}

func TestSeeIp_NonOKStatus(t *testing.T) {
	withSeeIPServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := SeeIp(context.Background())
	if err == nil {
		t.Fatal("expected an error for a non-200 status")
	}
}

func TestSeeIp_MalformedJSON(t *testing.T) {
	withSeeIPServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not-json{{{`))
	})

	_, err := SeeIp(context.Background())
	if err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

func TestSeeIp_Timeout(t *testing.T) {
	withSeeIPServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(`{"ip":"203.0.113.42"}`))
	})

	client := &http.Client{Timeout: 20 * time.Millisecond}
	_, err := SeeIpWithClient(context.Background(), client)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
}

func TestSeeIp_ContextCancelled(t *testing.T) {
	withSeeIPServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(`{"ip":"203.0.113.42"}`))
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := SeeIp(ctx)
	if err == nil {
		t.Fatal("expected an error for an already-cancelled context")
	}
}
