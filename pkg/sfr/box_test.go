package sfr

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestGetXMLTimeout verifies that getXML honours the sfrHTTPClient timeout and
// does not block indefinitely when the server is slow/unreachable.
func TestGetXMLTimeout(t *testing.T) {
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-hang
	}))
	t.Cleanup(func() {
		close(hang)
		srv.Close()
	})

	old := sfrHTTPClient
	sfrHTTPClient = &http.Client{Timeout: 100 * time.Millisecond}
	t.Cleanup(func() { sfrHTTPClient = old })

	start := time.Now()
	_, err := getXML(srv.URL)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("getXML blocked for %v; expected < 2s with 100ms timeout", elapsed)
	}
}

// TestGetXMLSuccess verifies that getXML returns body bytes on a normal 200 response.
func TestGetXMLSuccess(t *testing.T) {
	body := []byte(`<rsp stat="ok" version="1.0"><lan ip_addr="192.168.1.1" netmask="255.255.255.0" dhcp_active="on" dhcp_start="192.168.1.20" dhcp_end="192.168.1.100" dhcp_lease="86400"/></rsp>`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	data, err := getXML(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != string(body) {
		t.Errorf("got %q, want %q", data, body)
	}
}

// TestGetXMLNon200 verifies that getXML returns an error for non-200 status codes.
func TestGetXMLNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	_, err := getXML(srv.URL)
	if err == nil {
		t.Fatal("expected error for 503 response, got nil")
	}
}
