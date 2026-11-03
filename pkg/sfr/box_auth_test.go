package sfr

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-logr/logr/funcr"
)

// hostRedirectTransport rewrites every request's host to targetURL, allowing
// queryBox (which hardcodes http://{ip}/api/1.0/) to be tested with a local
// httptest.Server without changing production code.
type hostRedirectTransport struct {
	targetURL string
}

func (r *hostRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, _ := url.Parse(r.targetURL)
	redirected := req.Clone(req.Context())
	redirected.URL.Scheme = target.Scheme
	redirected.URL.Host = target.Host
	return http.DefaultTransport.RoundTrip(redirected)
}

// setupMockSFR registers a test HTTP server whose responses replace the real
// SFR box. Returns a dummy IP that queryBox will use to build its URL (the
// redirect transport forwards the request to the test server regardless).
func setupMockSFR(t *testing.T, handler http.HandlerFunc) net.IP {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	old := sfrHTTPClient
	sfrHTTPClient = &http.Client{Transport: &hostRedirectTransport{targetURL: srv.URL}}
	t.Cleanup(func() { sfrHTTPClient = old })

	return net.ParseIP("192.168.1.1")
}

// TestInit verifies that Init sets and clears the package-level credentials.
func TestInit(t *testing.T) {
	t.Cleanup(func() { Init("", "") })

	Init("user@example.com", "s3cret")
	if username != "user@example.com" {
		t.Errorf("username = %q, want %q", username, "user@example.com")
	}
	if password != "s3cret" {
		t.Errorf("password = %q, want %q", password, "s3cret")
	}

	Init("", "")
	if username != "" || password != "" {
		t.Error("Init with empty strings should clear credentials")
	}
}

// TestDoHash verifies that doHash produces a fixed-length hex string and is
// deterministic for the same inputs.
func TestDoHash(t *testing.T) {
	hash, err := doHash("admin", []byte("token"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hash) != 64 {
		t.Errorf("expected 64 hex chars, got %d: %q", len(hash), hash)
	}

	hash2, err := doHash("admin", []byte("token"))
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if hash != hash2 {
		t.Errorf("doHash is not deterministic: %q != %q", hash, hash2)
	}

	// Different inputs must produce different hashes.
	hash3, err := doHash("other", []byte("token"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash == hash3 {
		t.Error("different inputs produced the same hash")
	}
}

// TestQueryBoxLanInfo exercises the happy path of queryBox for a lan.getInfo
// response and verifies the XML is parsed into a *LanInfo.
func TestQueryBoxLanInfo(t *testing.T) {
	ip := setupMockSFR(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("method"); got != "lan.getInfo" {
			t.Errorf("method = %q, want lan.getInfo", got)
		}
		fmt.Fprint(w, `<rsp stat="ok" version="1.0"><lan ip_addr="192.168.1.1" netmask="255.255.255.0" dhcp_active="on" dhcp_start="192.168.1.20" dhcp_end="192.168.1.100" dhcp_lease="86400"/></rsp>`)
	})

	res, err := queryBox(ip, "lan.getInfo", &map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, ok := res.(*LanInfo)
	if !ok {
		t.Fatalf("expected *LanInfo, got %T", res)
	}
	if info.Ip.String() != "192.168.1.1" {
		t.Errorf("LanInfo.Ip = %s, want 192.168.1.1", info.Ip)
	}
}

// TestQueryBoxAuthResponse exercises the auth token response path.
func TestQueryBoxAuthResponse(t *testing.T) {
	ip := setupMockSFR(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rsp stat="ok" version="1.0"><auth token="abc123" method="none"/></rsp>`)
	})

	res, err := queryBox(ip, "auth.getToken", &map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	auth, ok := res.(*Auth)
	if !ok {
		t.Fatalf("expected *Auth, got %T", res)
	}
	if auth.Token != "abc123" {
		t.Errorf("token = %q, want abc123", auth.Token)
	}
}

// TestQueryBoxErrorResponse verifies that a stat="fail" XML body is converted
// to a non-nil error.
func TestQueryBoxErrorResponse(t *testing.T) {
	ip := setupMockSFR(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rsp stat="fail"><err code="10" msg="Method Not Permitted"/></rsp>`)
	})

	_, err := queryBox(ip, "lan.getInfo", &map[string]string{})
	if err == nil {
		t.Fatal("expected error for stat=fail response, got nil")
	}
}

// TestRenewTokenSkipsAuthWhenNoCreds verifies that renewToken does not call
// auth.checkToken when credentials are empty — the new behavior added in this
// PR to allow anonymous SFR box access.
func TestRenewTokenSkipsAuthWhenNoCreds(t *testing.T) {
	checkTokenCalled := false
	ip := setupMockSFR(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "auth.getToken":
			fmt.Fprint(w, `<rsp stat="ok" version="1.0"><auth token="tok-anon" method="passwd"/></rsp>`)
		case "auth.checkToken":
			checkTokenCalled = true
			fmt.Fprint(w, `<rsp stat="ok" version="1.0"><auth token="tok-anon" method="passwd"/></rsp>`)
		}
	})

	Init("", "")
	savedToken := token
	t.Cleanup(func() { token = savedToken })

	if err := renewToken(ip); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if checkTokenCalled {
		t.Error("auth.checkToken must not be called when credentials are empty")
	}
	if token != "tok-anon" {
		t.Errorf("token = %q, want tok-anon", token)
	}
}

// TestRenewTokenCallsAuthWithCreds verifies that renewToken calls
// auth.checkToken when credentials are set.
func TestRenewTokenCallsAuthWithCreds(t *testing.T) {
	checkTokenCalled := false
	ip := setupMockSFR(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("method") {
		case "auth.getToken":
			fmt.Fprint(w, `<rsp stat="ok" version="1.0"><auth token="tok-auth" method="passwd"/></rsp>`)
		case "auth.checkToken":
			checkTokenCalled = true
			fmt.Fprint(w, `<rsp stat="ok" version="1.0"><auth token="tok-auth" method="passwd"/></rsp>`)
		}
	})

	Init("admin", "pass")
	savedToken := token
	t.Cleanup(func() {
		Init("", "")
		token = savedToken
	})

	if err := renewToken(ip); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !checkTokenCalled {
		t.Error("auth.checkToken must be called when credentials are set")
	}
}

// TestCheckTokenDoesNotLogSecrets runs the auth flow against a mock box and
// asserts no log line carries the credentials, the session token or the hash.
// It fails on the pre-#620 code, which logged all four.
func TestCheckTokenDoesNotLogSecrets(t *testing.T) {
	t.Cleanup(func() { Init("", "") })
	Init("user@example.com", "s3cret")

	var logged strings.Builder
	oldLog := log
	log = funcr.New(func(prefix, args string) {
		logged.WriteString(prefix)
		logged.WriteByte(' ')
		logged.WriteString(args)
		logged.WriteByte('\n')
	}, funcr.Options{Verbosity: 10})
	t.Cleanup(func() { log = oldLog })

	ip := setupMockSFR(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if got := r.Form.Get("method"); got != "auth.checkToken" {
			t.Errorf("method = %q, want auth.checkToken", got)
		}
		// Echo the session token as an attribute, as auth.getToken's reply does.
		fmt.Fprint(w, `<rsp stat="ok" version="1.0"><auth token="tok-123"></auth></rsp>`)
	})

	if err := checkToken(ip, "tok-123"); err != nil {
		t.Fatalf("checkToken: %v", err)
	}

	uh, err := doHash("user@example.com", []byte("tok-123"))
	if err != nil {
		t.Fatalf("doHash(user): %v", err)
	}
	ph, err := doHash("s3cret", []byte("tok-123"))
	if err != nil {
		t.Fatalf("doHash(password): %v", err)
	}

	output := logged.String()
	for _, secret := range []string{"s3cret", "user@example.com", "tok-123", uh + ph} {
		if strings.Contains(output, secret) {
			t.Errorf("log output leaks %q:\n%s", secret, output)
		}
	}
	if !strings.Contains(output, "Calling") {
		t.Fatalf("expected the Calling line at verbosity 10:\n%s", output)
	}
}
